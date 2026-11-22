// Package auth authenticates the public API with HMAC, nonces, and a rate limit.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// HeaderKey is the client key id.
	HeaderKey = "X-MCon-Key"
	// HeaderTimestamp is a unix timestamp in seconds.
	HeaderTimestamp = "X-MCon-Timestamp"
	// HeaderNonce is a one-time request nonce.
	HeaderNonce = "X-MCon-Nonce"
	// HeaderSignature is the hex HMAC-SHA256 signature.
	HeaderSignature = "X-MCon-Signature"
	// HeaderIdempotency is the required idempotency key.
	HeaderIdempotency = "Idempotency-Key"
)

// ErrUnauthorized is returned for any failed credential check.
var ErrUnauthorized = errors.New("unauthorized")

// NonceStore remembers nonces until they expire.
type NonceStore interface {
	UseNonce(ctx context.Context, client, nonce string, expires time.Time) error
}

// Authenticator checks signed public requests.
type Authenticator struct {
	Now      func() time.Time
	Window   time.Duration
	NonceTTL time.Duration
	Nonces   NonceStore
	Secret   func(keyID string) (string, bool)
}

// Canonical is the string that is signed.
func Canonical(method, path, timestamp, nonce string, body []byte) string {
	sum := sha256.Sum256(body)
	return strings.ToUpper(method) + "\n" + path + "\n" + timestamp + "\n" + nonce + "\n" + hex.EncodeToString(sum[:])
}

// Sign returns the hex HMAC-SHA256 of canonical.
func Sign(secret, canonical string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}

// Verify reports whether sig matches canonical for secret.
func Verify(secret, canonical, sig string) bool {
	got, err := hex.DecodeString(strings.TrimSpace(sig))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	return hmac.Equal(mac.Sum(nil), got)
}

// VerifyRequest authenticates a public request and consumes its nonce.
func (a *Authenticator) VerifyRequest(ctx context.Context, method, path string, header http.Header, body []byte) (string, error) {
	keyID := strings.TrimSpace(header.Get(HeaderKey))
	ts := strings.TrimSpace(header.Get(HeaderTimestamp))
	nonce := strings.TrimSpace(header.Get(HeaderNonce))
	sig := strings.TrimSpace(header.Get(HeaderSignature))
	if keyID == "" || ts == "" || nonce == "" || sig == "" || a.Secret == nil {
		return "", ErrUnauthorized
	}
	secret, ok := a.Secret(keyID)
	if !ok || secret == "" {
		return "", ErrUnauthorized
	}
	if !validNonce(nonce) {
		return "", ErrUnauthorized
	}
	unix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return "", ErrUnauthorized
	}
	now := time.Now()
	if a.Now != nil {
		now = a.Now()
	}
	delta := now.Sub(time.Unix(unix, 0))
	if delta < 0 {
		delta = -delta
	}
	window := a.Window
	if window <= 0 {
		window = 5 * time.Minute
	}
	if delta > window {
		return "", ErrUnauthorized
	}
	if !Verify(secret, Canonical(method, path, ts, nonce, body), sig) {
		return "", ErrUnauthorized
	}
	ttl := a.NonceTTL
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	if a.Nonces != nil {
		if err := a.Nonces.UseNonce(ctx, keyID, nonce, now.Add(ttl)); err != nil {
			return "", ErrUnauthorized
		}
	}
	return keyID, nil
}

func validNonce(nonce string) bool {
	if len(nonce) < 8 || len(nonce) > 128 {
		return false
	}
	for _, r := range nonce {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '~' || r == '-':
		default:
			return false
		}
	}
	return true
}

// ValidIdempotencyKey reports whether key may be used as an idempotency key.
func ValidIdempotencyKey(key string) bool {
	if len(key) < 1 || len(key) > 200 {
		return false
	}
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == ':' || r == '-':
		default:
			return false
		}
	}
	return true
}

// TokenEqual compares bearer tokens in constant time when the lengths match.
func TokenEqual(a, b string) bool {
	return hmac.Equal([]byte(a), []byte(b))
}

// Limiter is an in-memory token bucket keyed by client or IP.
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	tokens  float64
	updated time.Time
}

// NewLimiter returns an empty limiter.
func NewLimiter() *Limiter {
	return &Limiter{buckets: map[string]*bucket{}}
}

// Allow consumes one token when the bucket has one available.
func (l *Limiter) Allow(key string, rps float64, burst int, now time.Time) bool {
	if l == nil || rps <= 0 || burst < 1 {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[key]
	if b == nil {
		b = &bucket{tokens: float64(burst), updated: now}
		l.buckets[key] = b
	}
	elapsed := now.Sub(b.updated).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * rps
	}
	if b.tokens > float64(burst) {
		b.tokens = float64(burst)
	}
	b.updated = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
