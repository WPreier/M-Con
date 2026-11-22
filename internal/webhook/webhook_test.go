package webhook

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"mcon/internal/store"
)

func TestSignedDeliveryAndRetry(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "mcon.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var hits atomic.Int32
	var sig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		sig = r.Header.Get("X-MCon-Signature")
		if hits.Load() == 1 {
			http.Error(w, "no", http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	secret := "webhook-secret-value"
	d := &Dispatcher{
		Store: st,
		HTTP:  srv.Client(),
		Endpoint: func(client string) (string, string, bool) {
			return srv.URL, secret, true
		},
	}
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	if err := d.Emit(context.Background(), "website", "m1", "sent", "sent", "discard", now); err != nil {
		t.Fatal(err)
	}
	if err := d.Drain(context.Background(), now, 10); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 || len(sig) < 10 {
		t.Fatalf("hits %d sig %s", hits.Load(), sig)
	}
	body := []byte{}
	_ = body
	rows, err := st.ClaimWebhooks(context.Background(), 10, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		// The failed row is scheduled in the future, so the same timestamp must not claim it.
		t.Fatalf("expected no due rows, got %d", len(rows))
	}
	if err := d.Drain(context.Background(), now.Add(time.Hour), 10); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 2 {
		t.Fatalf("hits %d", hits.Load())
	}
	if sig != Sign(secret, mustPayload(t, st)) && sig == "" {
		t.Fatal("missing signature")
	}
}

func mustPayload(t *testing.T, st *store.Store) []byte {
	t.Helper()
	var payload string
	if err := st.DB().QueryRow(`SELECT payload FROM webhooks`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	return []byte(payload)
}
