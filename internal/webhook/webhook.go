// Package webhook stores and delivers signed delivery events.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"mcon/internal/logging"
	"mcon/internal/metrics"
	"mcon/internal/store"
)

// Endpoint resolves the callback URL and secret for a client.
type Endpoint func(clientKey string) (url, secret string, ok bool)

// Dispatcher is the signed webhook outbox.
type Dispatcher struct {
	Store    *store.Store
	Endpoint Endpoint
	HTTP     *http.Client
	Metrics  *metrics.Metrics
	Log      *logging.Logger
}

// Sign returns the sha256= HMAC header value.
func Sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// Emit queues an event when the client has a webhook URL.
func (d *Dispatcher) Emit(ctx context.Context, client, messageID, event, state, providerName string, now time.Time) error {
	if d == nil || d.Endpoint == nil || d.Store == nil {
		return nil
	}
	url, secret, ok := d.Endpoint(client)
	if !ok || url == "" || secret == "" {
		return nil
	}
	payload, err := json.Marshal(map[string]string{
		"id": messageID, "event": event, "state": state, "provider": providerName,
		"at": now.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return err
	}
	return d.Store.EnqueueWebhook(ctx, client, messageID, event, string(payload), now)
}

// Drain delivers due outbox rows.
func (d *Dispatcher) Drain(ctx context.Context, now time.Time, limit int) error {
	if d == nil || d.Store == nil {
		return nil
	}
	rows, err := d.Store.ClaimWebhooks(ctx, limit, now)
	if err != nil {
		return err
	}
	for _, row := range rows {
		d.deliver(ctx, row, now)
	}
	return nil
}

func (d *Dispatcher) deliver(ctx context.Context, row store.Webhook, now time.Time) {
	url, secret, ok := d.Endpoint(row.ClientKey)
	if !ok || url == "" {
		_ = d.Store.FinishWebhook(ctx, row.ID, "dead", "missing endpoint", nil)
		return
	}
	body := []byte(row.Payload)
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		d.fail(ctx, row, now, err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MCon-Event", row.Event)
	req.Header.Set("X-MCon-Signature", Sign(secret, body))
	req.Header.Set("X-MCon-Timestamp", strconv.FormatInt(now.Unix(), 10))
	client := d.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		d.fail(ctx, row, now, err.Error())
		return
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		_ = d.Store.FinishWebhook(ctx, row.ID, "sent", "", nil)
		if d.Metrics != nil {
			d.Metrics.Inc("mcon_webhook_results_total", map[string]string{"result": "sent"})
		}
		return
	}
	d.fail(ctx, row, now, "http "+strconv.Itoa(res.StatusCode))
}

func backoff(attempt int) time.Duration {
	d := 5 * time.Second
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= time.Hour {
			return time.Hour
		}
	}
	if d > time.Hour {
		return time.Hour
	}
	return d
}

func (d *Dispatcher) fail(ctx context.Context, row store.Webhook, now time.Time, msg string) {
	if d.Metrics != nil {
		d.Metrics.Inc("mcon_webhook_results_total", map[string]string{"result": "retry"})
	}
	if d.Log != nil {
		d.Log.Warn("webhook failed", "message_id", row.MessageID, "event", row.Event, "error", msg)
	}
	if row.Attempts >= row.MaxAttempts {
		_ = d.Store.FinishWebhook(ctx, row.ID, "dead", msg, nil)
		return
	}
	next := now.Add(backoff(row.Attempts))
	_ = d.Store.FinishWebhook(ctx, row.ID, "pending", msg, &next)
}
