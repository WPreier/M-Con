// Package httpx shares JSON and form calls used by the HTTP provider adapters.
package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"mcon/internal/provider"
)

// Do sends one request and returns the status, body, and headers.
func Do(ctx context.Context, client *http.Client, method, url string, headers map[string]string, body []byte) (int, []byte, http.Header, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("Content-Type") == "" && body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := client.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer res.Body.Close()
	buf, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return res.StatusCode, nil, res.Header, err
	}
	return res.StatusCode, buf, res.Header, nil
}

// Classify maps an HTTP status onto a provider error. 2xx is success.
func Classify(name string, status int, body []byte) error {
	if status >= 200 && status < 300 {
		return nil
	}
	msg := strings.TrimSpace(string(body))
	if len(msg) > 180 {
		msg = msg[:180]
	}
	msg = strings.ReplaceAll(msg, "\n", " ")
	if msg == "" {
		msg = fmt.Sprintf("http %d", status)
	}
	if status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500 {
		return provider.Transient(name, msg, status)
	}
	return provider.Permanent(name, msg, status)
}

// PostJSON marshals payload and sends it as JSON.
func PostJSON(ctx context.Context, client *http.Client, method, endpoint string, headers map[string]string, payload any) (int, []byte, http.Header, error) {
	var body []byte
	var err error
	if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			return 0, nil, nil, err
		}
	}
	return Do(ctx, client, method, endpoint, headers, body)
}

// StringField reads the first present string field from a JSON object.
func StringField(body []byte, keys ...string) string {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return ""
	}
	for _, key := range keys {
		if s, ok := m[key].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// WithTimeout returns a child context capped by timeout.
func WithTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return context.WithTimeout(ctx, timeout)
}
