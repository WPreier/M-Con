// Package httpapi serves the public HMAC API and the localhost admin API.
package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"mcon/internal/archive"
	"mcon/internal/auth"
	"mcon/internal/config"
	"mcon/internal/id"
	"mcon/internal/message"
	"mcon/internal/metrics"
	"mcon/internal/service"
	"mcon/internal/store"
	testui "mcon/web/test"
)

// RequestAuth checks a signed public request.
type RequestAuth interface {
	VerifyRequest(ctx context.Context, method, path string, header http.Header, body []byte) (string, error)
}

// API is the HTTP surface of the daemon.
type API struct {
	Snap    func() config.Snapshot
	Store   *store.Store
	Auth    RequestAuth
	Limit   *auth.Limiter
	Svc     *service.Service
	Metrics *metrics.Metrics
	Archive archive.Archive
	Ready   func(r *http.Request) error
	Reload  func() error
	Version string
	Now     func() time.Time
}

func (a *API) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

// Public is the HMAC listener.
func (a *API) Public() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/messages", a.create)
	mux.HandleFunc("GET /v1/messages/{id}", a.status)
	mux.HandleFunc("POST /v1/messages/{id}/cancel", a.cancel)
	return mux
}

// Admin is the localhost control listener.
func (a *API) Admin() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.health)
	mux.HandleFunc("GET /readyz", a.ready)
	mux.HandleFunc("GET /version", a.version)
	mux.HandleFunc("GET /metrics", a.metrics)
	mux.HandleFunc("GET /test", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/test/", http.StatusFound)
	})
	mux.Handle("GET /test/", a.testPage())
	mux.HandleFunc("GET /admin/v1/messages", a.admin(a.list))
	mux.HandleFunc("GET /admin/v1/messages/{id}", a.admin(a.detail))
	mux.HandleFunc("GET /admin/v1/messages/{id}/eml", a.admin(a.eml))
	mux.HandleFunc("POST /admin/v1/messages/{id}/resend", a.admin(a.resend))
	mux.HandleFunc("POST /admin/v1/messages/{id}/replay", a.admin(a.replay))
	mux.HandleFunc("GET /admin/v1/suppressions", a.admin(a.listSuppress))
	mux.HandleFunc("POST /admin/v1/suppressions", a.admin(a.addSuppress))
	mux.HandleFunc("DELETE /admin/v1/suppressions", a.admin(a.delSuppress))
	mux.HandleFunc("POST /admin/v1/test/send", a.admin(a.testSend))
	mux.HandleFunc("POST /admin/v1/reload", a.admin(a.reload))
	return a.allow(mux)
}

func (a *API) allow(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.ipAllowed(r) {
			writeErr(w, http.StatusForbidden, "forbidden")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *API) ipAllowed(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, cidr := range a.Snap().Config.Security.AdminAllowCIDR {
		_, network, err := net.ParseCIDR(cidr)
		if err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

func (a *API) admin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearer(r.Header.Get("Authorization"))
		if !auth.TokenEqual(token, a.Snap().Secrets.Admin.Token) {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r)
	}
}

func bearer(v string) string {
	const p = "Bearer "
	if strings.HasPrefix(v, p) {
		return strings.TrimSpace(v[len(p):])
	}
	return ""
}

func (a *API) create(w http.ResponseWriter, r *http.Request) {
	body, ok := a.readBody(w, r)
	if !ok {
		return
	}
	if !strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		writeErr(w, http.StatusBadRequest, "content type must be application/json")
		return
	}
	client, ok := a.authorize(w, r, body)
	if !ok {
		return
	}
	key := strings.TrimSpace(r.Header.Get(auth.HeaderIdempotency))
	if !auth.ValidIdempotencyKey(key) {
		writeErr(w, http.StatusBadRequest, "Idempotency-Key is required")
		return
	}
	sub, err := decodeSubmission(body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	status, resp, err := a.Svc.Accept(r.Context(), client, key, body, sub)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(resp)
}

func (a *API) status(w http.ResponseWriter, r *http.Request) {
	client, ok := a.authorize(w, r, nil)
	if !ok {
		return
	}
	msg, err := a.Store.GetMeta(r.Context(), r.PathValue("id"))
	if err != nil || msg.ClientKey != client {
		writeErr(w, http.StatusNotFound, "message not found")
		return
	}
	writeJSON(w, http.StatusOK, publicStatus(msg))
}

func (a *API) cancel(w http.ResponseWriter, r *http.Request) {
	body, ok := a.readBody(w, r)
	if !ok {
		return
	}
	client, ok := a.authorize(w, r, body)
	if !ok {
		return
	}
	if err := a.Svc.Cancel(r.Context(), client, r.PathValue("id")); err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": r.PathValue("id"), "state": "cancelled"})
}

func (a *API) authorize(w http.ResponseWriter, r *http.Request, body []byte) (string, bool) {
	if !a.allowRate("ip:"+remoteIP(r), 10, 20) {
		writeErr(w, http.StatusTooManyRequests, "rate limit")
		return "", false
	}
	client, err := a.Auth.VerifyRequest(r.Context(), r.Method, r.URL.Path, r.Header, body)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return "", false
	}
	cl := a.Snap().Config.Clients[client]
	if !a.allowRate("client:"+client, cl.RateLimit.RPS, cl.RateLimit.Burst) {
		writeErr(w, http.StatusTooManyRequests, "rate limit")
		return "", false
	}
	return client, true
}

func (a *API) allowRate(key string, rps float64, burst int) bool {
	if a.Limit == nil {
		return true
	}
	return a.Limit.Allow(key, rps, burst, a.now())
}

func (a *API) readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	max := a.Snap().Config.Server.MaxBodyBytes
	if max <= 0 {
		max = 1 << 20
	}
	r.Body = http.MaxBytesReader(w, r.Body, max)
	buf, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusRequestEntityTooLarge, "body too large")
		return nil, false
	}
	return buf, true
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) ready(w http.ResponseWriter, r *http.Request) {
	if a.Ready != nil {
		if err := a.Ready(r); err != nil {
			writeErr(w, http.StatusServiceUnavailable, "not ready")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (a *API) version(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": a.Version})
}

func (a *API) metrics(w http.ResponseWriter, r *http.Request) {
	if !a.Snap().Config.Metrics.Enabled {
		http.NotFound(w, r)
		return
	}
	counts, _ := a.Store.CountByState(r.Context())
	gauges := map[string]float64{}
	depth := 0
	for _, state := range []string{"received", "spam_check", "queued", "retry_wait", "sending", "sent", "blocked", "dead", "cancelled"} {
		n := counts[state]
		gauges["mcon_messages_current{state=\""+state+"\"}"] = float64(n)
		switch state {
		case "received", "spam_check", "queued", "retry_wait", "sending":
			depth += n
		}
	}
	gauges["mcon_queue_depth"] = float64(depth)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = io.WriteString(w, a.Metrics.Render(gauges))
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	msgs, err := a.Store.List(r.Context(), r.URL.Query().Get("client"), r.URL.Query().Get("state"), limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list failed")
		return
	}
	out := make([]any, 0, len(msgs))
	for _, msg := range msgs {
		out = append(out, adminView(msg))
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": out})
}

func (a *API) detail(w http.ResponseWriter, r *http.Request) {
	msg, err := a.Store.GetMeta(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "message not found")
		return
	}
	writeJSON(w, http.StatusOK, adminView(msg))
}

func (a *API) eml(w http.ResponseWriter, r *http.Request) {
	msg, err := a.Store.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "message not found")
		return
	}
	var raw []byte
	if msg.RFC822Path != "" {
		raw, err = a.Archive.Read(msg.RFC822Path)
	}
	if err != nil || len(raw) == 0 {
		to, cc, bcc := split(msg)
		var atts []message.Attachment
		for _, att := range msg.Attachments {
			atts = append(atts, message.Attachment{Filename: att.Filename, ContentType: att.ContentType, Data: att.Data})
		}
		raw, err = message.Build(message.Outbound{
			ID: msg.ID, From: msg.From, To: to, Cc: cc, Bcc: bcc, ReplyTo: msg.ReplyTo,
			Subject: msg.Subject, Text: msg.Text, HTML: msg.HTML, Headers: msg.Headers,
			Attachments: atts, MessageID: msg.RFCMessageID,
		})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "eml unavailable")
			return
		}
	}
	w.Header().Set("Content-Type", "message/rfc822")
	_, _ = w.Write(raw)
}

func (a *API) resend(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SkipSpam bool `json:"skip_spam"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
			writeErr(w, http.StatusBadRequest, "invalid json")
			return
		}
	}
	msg, err := a.Svc.Resend(r.Context(), r.PathValue("id"), body.SkipSpam)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, adminView(msg))
}

func (a *API) replay(w http.ResponseWriter, r *http.Request) {
	if err := a.Svc.Replay(r.Context(), r.PathValue("id")); err != nil {
		writeServiceErr(w, err)
		return
	}
	msg, err := a.Store.GetMeta(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "message not found")
		return
	}
	writeJSON(w, http.StatusOK, adminView(msg))
}

func (a *API) listSuppress(w http.ResponseWriter, r *http.Request) {
	items, err := a.Store.ListSuppressions(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"suppressions": items})
}

func (a *API) addSuppress(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Address string `json:"address"`
		Reason  string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !strings.Contains(body.Address, "@") {
		writeErr(w, http.StatusBadRequest, "address is required")
		return
	}
	if err := a.Store.Suppress(r.Context(), strings.ToLower(body.Address), body.Reason, a.now()); err != nil {
		writeErr(w, http.StatusInternalServerError, "suppress failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) delSuppress(w http.ResponseWriter, r *http.Request) {
	addr := strings.ToLower(r.URL.Query().Get("address"))
	if err := a.Store.Unsuppress(r.Context(), addr); err != nil {
		writeErr(w, http.StatusNotFound, "address not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) testSend(w http.ResponseWriter, r *http.Request) {
	if !a.Snap().Config.TestUI.Enabled {
		writeErr(w, http.StatusNotFound, "test ui disabled")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, a.Snap().Config.Server.MaxBodyBytes))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	sub, err := decodeSubmission(body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	client := a.Snap().Config.TestUI.ClientKeyID
	status, resp, err := a.Svc.Accept(r.Context(), client, id.New(), body, sub)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(resp)
}

func (a *API) reload(w http.ResponseWriter, r *http.Request) {
	if a.Reload == nil {
		writeErr(w, http.StatusNotImplemented, "reload unavailable")
		return
	}
	if err := a.Reload(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "reloaded"})
}

func (a *API) testPage() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.Snap().Config.TestUI.Enabled {
			http.NotFound(w, r)
			return
		}
		http.StripPrefix("/test/", http.FileServer(http.FS(testui.Files))).ServeHTTP(w, r)
	})
}

type createRequest struct {
	From        string            `json:"from"`
	To          []string          `json:"to"`
	Cc          []string          `json:"cc"`
	Bcc         []string          `json:"bcc"`
	ReplyTo     string            `json:"reply_to"`
	Subject     string            `json:"subject"`
	Text        string            `json:"text"`
	HTML        string            `json:"html"`
	Attachments []attachmentIn    `json:"attachments"`
	Headers     map[string]string `json:"headers"`
	Tags        []string          `json:"tags"`
	Metadata    map[string]any    `json:"metadata"`
	Spam        *struct {
		Hint string `json:"hint"`
	} `json:"spam"`
	Provider string `json:"provider"`
}

type attachmentIn struct {
	Filename      string `json:"filename"`
	ContentType   string `json:"content_type"`
	ContentBase64 string `json:"content_base64"`
}

func decodeSubmission(body []byte) (message.Submission, error) {
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	var req createRequest
	if err := dec.Decode(&req); err != nil {
		return message.Submission{}, errors.New("invalid json")
	}
	if dec.More() {
		return message.Submission{}, errors.New("invalid json")
	}
	sub := message.Submission{
		From: req.From, To: req.To, Cc: req.Cc, Bcc: req.Bcc, ReplyTo: req.ReplyTo,
		Subject: req.Subject, Text: req.Text, HTML: req.HTML, Headers: req.Headers,
		Tags: req.Tags, Metadata: req.Metadata, Provider: req.Provider,
	}
	if req.Spam != nil {
		sub.SpamHint = req.Spam.Hint
	}
	for _, att := range req.Attachments {
		data, err := base64.StdEncoding.DecodeString(att.ContentBase64)
		if err != nil {
			return message.Submission{}, errors.New("attachment is not base64")
		}
		sub.Attachments = append(sub.Attachments, message.Attachment{Filename: att.Filename, ContentType: att.ContentType, Data: data})
	}
	return sub, nil
}

func publicStatus(msg *store.Message) map[string]any {
	out := map[string]any{
		"id": msg.ID, "state": msg.State, "provider": msg.Provider,
		"content_sha256": msg.ContentSHA256, "attempts": msg.Attempts, "last_error": msg.LastError,
		"created_at": msg.CreatedAt, "updated_at": msg.UpdatedAt,
	}
	if msg.SentAt != nil {
		out["sent_at"] = msg.SentAt
	}
	return out
}

func adminView(msg *store.Message) map[string]any {
	out := publicStatus(msg)
	out["from"] = msg.From
	out["subject"] = msg.Subject
	out["client"] = msg.ClientKey
	out["body_sha256"] = msg.BodySHA256
	out["resend_of"] = msg.ResendOf
	out["spam_verdict"] = msg.SpamVerdict
	out["spam_reason"] = msg.SpamReason
	out["spam_engine"] = msg.SpamEngine
	out["recipients"] = msg.Recipients
	if msg.SpamScore != nil {
		out["spam_score"] = *msg.SpamScore
	}
	return out
}

func split(msg *store.Message) (to, cc, bcc []string) {
	for _, rcpt := range msg.Recipients {
		switch rcpt.Kind {
		case "cc":
			cc = append(cc, rcpt.Address)
		case "bcc":
			bcc = append(bcc, rcpt.Address)
		default:
			to = append(to, rcpt.Address)
		}
	}
	return to, cc, bcc
}

func writeServiceErr(w http.ResponseWriter, err error) {
	var se *service.StatusError
	if errors.As(err, &se) {
		writeErr(w, se.Status, se.Msg)
		return
	}
	writeErr(w, http.StatusInternalServerError, "internal error")
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
