package httpapi

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"mcon/internal/archive"
	"mcon/internal/auth"
	"mcon/internal/config"
	"mcon/internal/metrics"
	"mcon/internal/service"
	"mcon/internal/store"
)

func newAPI(t *testing.T) (*API, *store.Store, config.Snapshot) {
	t.Helper()
	dir := t.TempDir()
	cfg, sec := config.Local(dir)
	if err := config.Prepare(cfg); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(cfg.Paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	snap := config.Snapshot{Config: cfg, Secrets: sec}
	api := &API{
		Snap:  func() config.Snapshot { return snap },
		Store: st,
		Auth: &auth.Authenticator{
			Now: func() time.Time { return time.Unix(1_700_000_000, 0) }, Window: time.Minute, NonceTTL: time.Minute, Nonces: st,
			Secret: func(id string) (string, bool) {
				s, ok := sec.Clients[id]
				return s.HMACSecret, ok
			},
		},
		Limit: auth.NewLimiter(),
		Svc: &service.Service{
			Store: st, Archive: archive.Archive{Dir: cfg.Paths.ArchiveDir},
			Cfg: func() config.Snapshot { return snap }, Metrics: metrics.New(),
			Now: func() time.Time { return time.Unix(1_700_000_000, 0).UTC() },
		},
		Metrics: metrics.New(),
		Version: "0.1.0",
	}
	return api, st, snap
}

func TestAcceptIdempotencyAndInjection(t *testing.T) {
	api, st, snap := newAPI(t)
	body := []byte(`{"from":"noreply@example.com","to":["user@example.com"],"subject":"Hi","text":"hello","headers":{"X-Form":"contact"}}`)
	pub := httptest.NewServer(api.Public())
	defer pub.Close()
	secret := snap.Secrets.Clients["website"].HMACSecret
	do := func(key string, payload []byte, nonce string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, pub.URL+"/v1/messages", bytes.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		ts := "1700000000"
		req.Header.Set(auth.HeaderKey, "website")
		req.Header.Set(auth.HeaderTimestamp, ts)
		req.Header.Set(auth.HeaderNonce, nonce)
		req.Header.Set(auth.HeaderSignature, auth.Sign(secret, auth.Canonical(http.MethodPost, "/v1/messages", ts, nonce, payload)))
		req.Header.Set(auth.HeaderIdempotency, key)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	resp := do("idem-1", body, "nonce-accept-1")
	buf, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted || !strings.Contains(string(buf), `"state":"received"`) {
		t.Fatalf("accept %d %s", resp.StatusCode, buf)
	}
	resp = do("idem-1", body, "nonce-accept-2")
	buf, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("replay %d %s", resp.StatusCode, buf)
	}
	other := []byte(`{"from":"noreply@example.com","to":["user@example.com"],"subject":"Other","text":"hello"}`)
	resp = do("idem-1", other, "nonce-accept-3")
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("conflict %d", resp.StatusCode)
	}
	bad := []byte(`{"from":"noreply@example.com","to":["user@example.com"],"subject":"Hi\nBcc: evil@x","text":"hello"}`)
	resp = do("idem-2", bad, "nonce-accept-4")
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("injection %d", resp.StatusCode)
	}
	req, _ := http.NewRequest(http.MethodPost, pub.URL+"/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("auth %d", resp.StatusCode)
	}
	var n int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("rows %d %v", n, err)
	}
	var path string
	if err := st.DB().QueryRow(`SELECT rfc822_path FROM messages`).Scan(&path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(path); err != nil {
		t.Fatal(err)
	}
	adm := httptest.NewServer(api.Admin())
	defer adm.Close()
	page, err := http.Get(adm.URL + "/test/")
	if err != nil {
		t.Fatal(err)
	}
	defer page.Body.Close()
	html, _ := io.ReadAll(page.Body)
	if page.StatusCode != 200 || !strings.Contains(string(html), "Subject") {
		t.Fatalf("test page %d %s", page.StatusCode, html)
	}
	req, _ = http.NewRequest(http.MethodGet, adm.URL+"/admin/v1/messages", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("admin %d", resp.StatusCode)
	}
}
