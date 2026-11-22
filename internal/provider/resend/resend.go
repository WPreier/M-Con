// Package resend delivers mail through the Resend API.
package resend

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"mcon/internal/message"
	"mcon/internal/provider"
	"mcon/internal/provider/httpx"
)

// Client is a Resend adapter.
type Client struct {
	BaseURL string
	APIKey  string
	Timeout time.Duration
	HTTP    *http.Client
}

// Send posts one email.
func (c *Client) Send(ctx context.Context, msg message.Outbound) (provider.Result, error) {
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = "https://api.resend.com"
	}
	payload := map[string]any{"from": msg.From, "to": msg.To, "subject": msg.Subject, "text": msg.Text, "html": msg.HTML}
	if len(msg.Cc) > 0 {
		payload["cc"] = msg.Cc
	}
	if len(msg.Bcc) > 0 {
		payload["bcc"] = msg.Bcc
	}
	if msg.ReplyTo != "" {
		payload["reply_to"] = msg.ReplyTo
	}
	buf, _ := json.Marshal(payload)
	ctx, cancel := httpx.WithTimeout(ctx, c.Timeout)
	defer cancel()
	status, body, _, err := httpx.Do(ctx, c.HTTP, http.MethodPost, base+"/emails", map[string]string{
		"Authorization": "Bearer " + c.APIKey,
		"Content-Type":  "application/json",
	}, buf)
	if err != nil {
		return provider.Result{}, provider.Transient("resend", err.Error(), 0)
	}
	if err := httpx.Classify("resend", status, body); err != nil {
		return provider.Result{}, err
	}
	var out struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(body, &out)
	return provider.Result{ProviderMessageID: out.ID}, nil
}
