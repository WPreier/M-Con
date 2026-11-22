// Package service accepts, cancels, resends, and replays messages.
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"mcon/internal/archive"
	"mcon/internal/config"
	"mcon/internal/id"
	"mcon/internal/message"
	"mcon/internal/metrics"
	"mcon/internal/store"
	"mcon/internal/webhook"
)

// StatusError is an HTTP-mappable failure.
type StatusError struct {
	Status int
	Msg    string
}

func (e *StatusError) Error() string {
	if e == nil {
		return ""
	}
	return e.Msg
}

// Service implements the mail acceptance workflow.
type Service struct {
	Store   *store.Store
	Archive archive.Archive
	Hooks   *webhook.Dispatcher
	Cfg     func() config.Snapshot
	Metrics *metrics.Metrics
	Now     func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

type acceptJSON struct {
	ID            string `json:"id"`
	State         string `json:"state"`
	ContentSHA256 string `json:"content_sha256"`
	Deduped       bool   `json:"deduped,omitempty"`
}

// Accept stores a message and returns the HTTP status plus JSON body.
func (s *Service) Accept(ctx context.Context, clientKey, idemKey string, rawBody []byte, sub message.Submission) (int, []byte, error) {
	snap := s.Cfg()
	cl, ok := snap.Config.Clients[clientKey]
	if !ok {
		return 0, nil, &StatusError{Status: 403, Msg: "unknown client"}
	}
	prov := sub.Provider
	if prov == "" {
		prov = snap.Config.Providers.Default
	}
	if _, ok := snap.Config.Providers.Items[prov]; !ok {
		return 0, nil, &StatusError{Status: 400, Msg: "unknown provider"}
	}
	if len(cl.AllowedProviders) > 0 && !contains(cl.AllowedProviders, prov) {
		return 0, nil, &StatusError{Status: 403, Msg: "provider is not allowed"}
	}
	if err := message.Validate(sub, message.Limits{
		MaxRecipients: cl.MaxRecipients, MaxMessageBytes: cl.MaxMessageBytes,
		MaxAttachments: cl.MaxAttachments, AllowedFrom: cl.AllowedFrom,
	}); err != nil {
		return 0, nil, validationError(err)
	}
	if meta, err := json.Marshal(sub.Metadata); err == nil && len(meta) > 8192 {
		return 0, nil, &StatusError{Status: 400, Msg: "metadata is too large"}
	}
	now := s.now()
	reqHash := sha256.Sum256(rawBody)
	hash := hex.EncodeToString(reqHash[:])
	if idemKey != "" {
		rec, err := s.Store.GetIdempotency(ctx, clientKey, idemKey)
		if err == nil && rec.ExpiresAt.After(now) {
			if rec.RequestHash != hash {
				return 0, nil, &StatusError{Status: 409, Msg: "idempotency key reused with a different body"}
			}
			return 200, []byte(rec.ResponseJSON), nil
		}
	}
	blocked := s.blocked(ctx, sub)
	bodyHash := message.BodyHash(sub)
	if !blocked && snap.Config.Dedupe.Enabled {
		since := now.Add(-snap.Config.Dedupe.Window.Std())
		if existingID, err := s.Store.FindDedupe(ctx, clientKey, bodyHash, since); err == nil {
			existing, err := s.Store.GetMeta(ctx, existingID)
			if err != nil {
				return 0, nil, err
			}
			resp, _ := json.Marshal(acceptJSON{ID: existing.ID, State: existing.State, ContentSHA256: existing.ContentSHA256, Deduped: true})
			s.remember(ctx, clientKey, idemKey, hash, existing.ID, resp, now, snap)
			return 202, resp, nil
		}
	}
	msgID := id.New()
	mid := message.MessageID(msgID, sub.From)
	raw, err := message.Build(message.Outbound{
		ID: msgID, From: sub.From, To: sub.To, Cc: sub.Cc, Bcc: sub.Bcc, ReplyTo: sub.ReplyTo,
		Subject: sub.Subject, Text: sub.Text, HTML: sub.HTML, Headers: sub.Headers,
		Attachments: sub.Attachments, MessageID: mid, Date: now,
	})
	if err != nil {
		return 0, nil, validationError(err)
	}
	var path string
	if snap.Config.Archive.Enabled {
		path, err = s.Archive.Write(msgID, raw, now)
		if err != nil {
			return 0, nil, err
		}
	}
	state := "received"
	verdict, reason, engine := "", "", ""
	var score *float64
	if blocked {
		state = "blocked"
		verdict, reason, engine = "block", "suppressed", "suppression"
		one := 1.0
		score = &one
	}
	resp, _ := json.Marshal(acceptJSON{ID: msgID, State: state, ContentSHA256: message.ContentHash(raw)})
	msg := &store.Message{
		ID: msgID, ClientKey: clientKey, State: state, Provider: prov,
		From: sub.From, ReplyTo: sub.ReplyTo, Subject: sub.Subject, Text: sub.Text, HTML: sub.HTML,
		Headers: sub.Headers, Tags: sub.Tags, Metadata: sub.Metadata, SpamHint: sub.SpamHint,
		ContentSHA256: message.ContentHash(raw), BodySHA256: bodyHash, RFC822Path: path, RFCMessageID: mid,
		MaxAttempts: snap.Config.Queue.MaxAttempts, SpamScore: score, SpamReason: reason, SpamVerdict: verdict, SpamEngine: engine,
		IdempotencyKey: idemKey, CreatedAt: now, UpdatedAt: now,
	}
	msg.Recipients = recipients(sub)
	for _, att := range sub.Attachments {
		sum := sha256.Sum256(att.Data)
		msg.Attachments = append(msg.Attachments, store.Attachment{
			Filename: att.Filename, ContentType: att.ContentType, SHA256: hex.EncodeToString(sum[:]),
			Size: len(att.Data), Data: att.Data,
		})
	}
	var idem *store.Idempotency
	if idemKey != "" {
		idem = &store.Idempotency{
			ClientKey: clientKey, Key: idemKey, RequestHash: hash, MessageID: msgID,
			ResponseJSON: string(resp), CreatedAt: now, ExpiresAt: now.Add(snap.Config.Idempotency.TTL.Std()),
		}
	}
	if err := s.Store.InsertNew(ctx, msg, idem); err != nil {
		s.Archive.Remove(path)
		if errors.Is(err, store.ErrConflict) && idemKey != "" {
			rec, gerr := s.Store.GetIdempotency(ctx, clientKey, idemKey)
			if gerr == nil && rec.ExpiresAt.After(now) && rec.RequestHash == hash {
				return 200, []byte(rec.ResponseJSON), nil
			}
			return 0, nil, &StatusError{Status: 409, Msg: "idempotency key reused with a different body"}
		}
		return 0, nil, err
	}
	event := "accepted"
	if state == "blocked" {
		event = "blocked"
	}
	if s.Hooks != nil {
		_ = s.Hooks.Emit(ctx, clientKey, msgID, event, state, prov, now)
	}
	if s.Metrics != nil {
		s.Metrics.Inc("mcon_messages_accepted_total", nil)
	}
	return 202, resp, nil
}

func (s *Service) remember(ctx context.Context, client, key, hash, messageID string, resp []byte, now time.Time, snap config.Snapshot) {
	if key == "" {
		return
	}
	_ = s.Store.SaveIdempotency(ctx, store.Idempotency{
		ClientKey: client, Key: key, RequestHash: hash, MessageID: messageID,
		ResponseJSON: string(resp), CreatedAt: now, ExpiresAt: now.Add(snap.Config.Idempotency.TTL.Std()),
	})
}

func (s *Service) blocked(ctx context.Context, sub message.Submission) bool {
	hit, err := s.Store.Suppressed(ctx, lower(message.AddressesOf(sub)))
	return err == nil && len(hit) > 0
}

// Cancel stops a message that has not been sent.
func (s *Service) Cancel(ctx context.Context, client, messageID string) error {
	now := s.now()
	if err := s.Store.Cancel(ctx, client, messageID, now); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return &StatusError{Status: 404, Msg: "message not found"}
		}
		if errors.Is(err, store.ErrState) {
			return &StatusError{Status: 409, Msg: "message cannot be cancelled"}
		}
		return err
	}
	if s.Hooks != nil {
		_ = s.Hooks.Emit(ctx, client, messageID, "cancelled", "cancelled", "", now)
	}
	return nil
}

// Resend creates a new queued copy. skipSpam is only available on this path.
func (s *Service) Resend(ctx context.Context, messageID string, skipSpam bool) (*store.Message, error) {
	src, err := s.Store.Get(ctx, messageID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, &StatusError{Status: 404, Msg: "message not found"}
		}
		return nil, err
	}
	snap := s.Cfg()
	now := s.now()
	msgID := id.New()
	mid := message.MessageID(msgID, src.From)
	var atts []message.Attachment
	for _, att := range src.Attachments {
		atts = append(atts, message.Attachment{Filename: att.Filename, ContentType: att.ContentType, Data: att.Data})
	}
	to, cc, bcc := split(src)
	raw, err := message.Build(message.Outbound{
		ID: msgID, From: src.From, To: to, Cc: cc, Bcc: bcc, ReplyTo: src.ReplyTo,
		Subject: src.Subject, Text: src.Text, HTML: src.HTML, Headers: src.Headers,
		Attachments: atts, MessageID: mid, Date: now,
	})
	if err != nil {
		return nil, err
	}
	var path string
	if snap.Config.Archive.Enabled {
		path, err = s.Archive.Write(msgID, raw, now)
		if err != nil {
			return nil, err
		}
	}
	msg := &store.Message{
		ID: msgID, ClientKey: src.ClientKey, State: "received", Provider: src.Provider,
		From: src.From, ReplyTo: src.ReplyTo, Subject: src.Subject, Text: src.Text, HTML: src.HTML,
		Headers: src.Headers, Tags: src.Tags, Metadata: src.Metadata, SpamHint: src.SpamHint,
		ContentSHA256: message.ContentHash(raw), BodySHA256: src.BodySHA256, RFC822Path: path, RFCMessageID: mid,
		MaxAttempts: snap.Config.Queue.MaxAttempts, ResendOf: src.ID, SkipSpam: skipSpam,
		CreatedAt: now, UpdatedAt: now, Recipients: src.Recipients, Attachments: src.Attachments,
	}
	if err := s.Store.InsertNew(ctx, msg, nil); err != nil {
		s.Archive.Remove(path)
		return nil, err
	}
	if s.Hooks != nil {
		_ = s.Hooks.Emit(ctx, msg.ClientKey, msg.ID, "accepted", "received", msg.Provider, now)
	}
	return msg, nil
}

// Replay queues a dead message again.
func (s *Service) Replay(ctx context.Context, messageID string) error {
	if err := s.Store.Replay(ctx, messageID, s.now()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return &StatusError{Status: 404, Msg: "message not found"}
		}
		if errors.Is(err, store.ErrState) {
			return &StatusError{Status: 409, Msg: "message is not dead"}
		}
		return err
	}
	return nil
}

func validationError(err error) error {
	msg := err.Error()
	status := 400
	if strings.Contains(msg, "not allowed") {
		status = 403
	}
	if strings.Contains(msg, "exceeds") || strings.Contains(msg, "too many") {
		status = 413
	}
	return &StatusError{Status: status, Msg: msg}
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func lower(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = strings.ToLower(s)
	}
	return out
}

func recipients(sub message.Submission) []store.Recipient {
	var out []store.Recipient
	for _, addr := range sub.To {
		out = append(out, store.Recipient{Kind: "to", Address: addr})
	}
	for _, addr := range sub.Cc {
		out = append(out, store.Recipient{Kind: "cc", Address: addr})
	}
	for _, addr := range sub.Bcc {
		out = append(out, store.Recipient{Kind: "bcc", Address: addr})
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
