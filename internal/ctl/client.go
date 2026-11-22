// Package ctl is the client used by mcon-ctl.
package ctl

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"mcon/internal/auth"
	"mcon/internal/id"
)

// Options selects the public and admin endpoints.
type Options struct {
	PublicURL  string
	AdminURL   string
	KeyID      string
	HMACSecret string
	AdminToken string
	Insecure   bool
	HTTP       *http.Client
}

func (o Options) client() *http.Client {
	if o.HTTP != nil {
		return o.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: o.Insecure, MinVersion: tls.VersionTLS12}}}
}

// Send posts a message body to the public API.
func Send(ctx context.Context, opt Options, body []byte, idemKey string) (string, error) {
	if idemKey == "" {
		idemKey = id.New()
	}
	res, err := opt.signed(ctx, http.MethodPost, "/v1/messages", body, idemKey)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	buf, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusAccepted && res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("send: %s: %s", res.Status, bytes.TrimSpace(buf))
	}
	return string(buf), nil
}

// Status reads one message.
func Status(ctx context.Context, opt Options, messageID string) (string, error) {
	res, err := opt.signed(ctx, http.MethodGet, "/v1/messages/"+url.PathEscape(messageID), nil, "")
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	buf, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status: %s: %s", res.Status, bytes.TrimSpace(buf))
	}
	return string(buf), nil
}

// Wait polls until the message reaches a terminal state or the timeout elapses.
func Wait(ctx context.Context, opt Options, messageID string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		body, err := Status(ctx, opt, messageID)
		if err != nil {
			return "", err
		}
		var env struct {
			State string `json:"state"`
		}
		_ = json.Unmarshal([]byte(body), &env)
		switch env.State {
		case "sent", "blocked", "dead", "cancelled":
			return body, nil
		}
		if time.Now().After(deadline) {
			return body, fmt.Errorf("timeout waiting for %s, last state %s", messageID, env.State)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// Resend asks the admin API for a new copy.
func Resend(ctx context.Context, opt Options, messageID string, skipSpam bool) (string, error) {
	buf, _ := json.Marshal(map[string]bool{"skip_spam": skipSpam})
	return opt.admin(ctx, http.MethodPost, "/admin/v1/messages/"+url.PathEscape(messageID)+"/resend", buf)
}

// Replay queues a dead message.
func Replay(ctx context.Context, opt Options, messageID string) (string, error) {
	return opt.admin(ctx, http.MethodPost, "/admin/v1/messages/"+url.PathEscape(messageID)+"/replay", []byte("{}"))
}

// Health checks the admin listener.
func Health(ctx context.Context, opt Options) (string, error) {
	var out bytes.Buffer
	for _, path := range []string{"/healthz", "/readyz"} {
		res, err := opt.client().Get(join(opt.AdminURL, path))
		if err != nil {
			return "", err
		}
		buf, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return "", fmt.Errorf("%s: %s", path, bytes.TrimSpace(buf))
		}
		out.Write(buf)
	}
	return out.String(), nil
}

// Version reads the admin version endpoint.
func Version(ctx context.Context, opt Options) (string, error) {
	res, err := opt.client().Get(join(opt.AdminURL, "/version"))
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	buf, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("version: %s", bytes.TrimSpace(buf))
	}
	var env struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(buf, &env); err != nil {
		return "", err
	}
	return env.Version, nil
}

func (o Options) signed(ctx context.Context, method, path string, body []byte, idem string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, join(o.PublicURL, path), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := id.New()
	req.Header.Set(auth.HeaderKey, o.KeyID)
	req.Header.Set(auth.HeaderTimestamp, ts)
	req.Header.Set(auth.HeaderNonce, nonce)
	req.Header.Set(auth.HeaderSignature, auth.Sign(o.HMACSecret, auth.Canonical(method, path, ts, nonce, body)))
	if idem != "" {
		req.Header.Set(auth.HeaderIdempotency, idem)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return o.client().Do(req)
}

func (o Options) admin(ctx context.Context, method, path string, body []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, method, join(o.AdminURL, path), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+o.AdminToken)
	req.Header.Set("Content-Type", "application/json")
	res, err := o.client().Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	buf, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		return "", fmt.Errorf("%s: %s", res.Status, bytes.TrimSpace(buf))
	}
	return string(buf), nil
}

func join(base, path string) string {
	return stringsTrimRight(base, "/") + path
}

func stringsTrimRight(s, cut string) string {
	for len(s) > 0 && len(cut) > 0 && s[len(s)-1] == cut[0] {
		s = s[:len(s)-1]
	}
	return s
}
