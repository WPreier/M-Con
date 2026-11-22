package auth

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"
)

type memNonce struct {
	seen map[string]struct{}
}

func (m *memNonce) UseNonce(ctx context.Context, client, nonce string, expires time.Time) error {
	if m.seen == nil {
		m.seen = map[string]struct{}{}
	}
	k := client + "\x00" + nonce
	if _, ok := m.seen[k]; ok {
		return errors.New("replay")
	}
	m.seen[k] = struct{}{}
	return nil
}

func TestHMACAndReplay(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	body := []byte(`{"subject":"hi"}`)
	secret := "test-hmac-secret-value"
	ts := strconv.FormatInt(now.Unix(), 10)
	nonce := "nonce-1234"
	sig := Sign(secret, Canonical("POST", "/v1/messages", ts, nonce, body))
	a := &Authenticator{
		Now:      func() time.Time { return now },
		Window:   time.Minute,
		NonceTTL: time.Minute,
		Nonces:   &memNonce{},
		Secret: func(id string) (string, bool) {
			if id == "website" {
				return secret, true
			}
			return "", false
		},
	}
	h := http.Header{}
	h.Set(HeaderKey, "website")
	h.Set(HeaderTimestamp, ts)
	h.Set(HeaderNonce, nonce)
	h.Set(HeaderSignature, sig)
	if id, err := a.VerifyRequest(context.Background(), "POST", "/v1/messages", h, body); err != nil || id != "website" {
		t.Fatalf("verify %s %v", id, err)
	}
	if _, err := a.VerifyRequest(context.Background(), "POST", "/v1/messages", h, body); err != ErrUnauthorized {
		t.Fatalf("replay %v", err)
	}
	h.Set(HeaderNonce, "nonce-5678")
	h.Set(HeaderSignature, "00")
	if _, err := a.VerifyRequest(context.Background(), "POST", "/v1/messages", h, body); err != ErrUnauthorized {
		t.Fatalf("bad sig %v", err)
	}
	old := strconv.FormatInt(now.Add(-2*time.Minute).Unix(), 10)
	h.Set(HeaderTimestamp, old)
	h.Set(HeaderNonce, "nonce-9999")
	h.Set(HeaderSignature, Sign(secret, Canonical("POST", "/v1/messages", old, "nonce-9999", body)))
	if _, err := a.VerifyRequest(context.Background(), "POST", "/v1/messages", h, body); err != ErrUnauthorized {
		t.Fatalf("window %v", err)
	}
}

func TestRateLimit(t *testing.T) {
	l := NewLimiter()
	now := time.Now()
	if !l.Allow("c", 1, 2, now) || !l.Allow("c", 1, 2, now) {
		t.Fatal("burst should allow two")
	}
	if l.Allow("c", 1, 2, now) {
		t.Fatal("burst exceeded")
	}
	if !l.Allow("c", 1, 2, now.Add(time.Second)) {
		t.Fatal("refill failed")
	}
}

func TestInjectionSignatureChanges(t *testing.T) {
	a := Sign("s", Canonical("POST", "/v1/messages", "1", "nonce-aaaa", []byte("subject")))
	b := Sign("s", Canonical("POST", "/v1/messages", "1", "nonce-aaaa", []byte("subject\nBcc: evil@x")))
	if a == b {
		t.Fatal("signature ignored body change")
	}
}
