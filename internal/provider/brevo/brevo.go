// Package brevo delivers mail through the Brevo SMTP API.
package brevo

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

// Client is a Brevo adapter.
type Client struct {
	BaseURL string
	APIKey  string
	Timeout time.Duration
	HTTP    *http.Client
}

// Send posts one transactional email.
func (c *Client) Send(ctx context.Context, msg message.Outbound) (provider.Result, error) {
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = "https://api.brevo.com"
	}
	payload := map[string]any{
		"sender":      map[string]string{"email": msg.From},
		"to":          emails(msg.To),
		"subject":     msg.Subject,
		"textContent": msg.Text,
		"htmlContent": msg.HTML,
	}
	if len(msg.Cc) > 0 {
		payload["cc"] = emails(msg.Cc)
	}
	if len(msg.Bcc) > 0 {
		payload["bcc"] = emails(msg.Bcc)
	}
	if msg.ReplyTo != "" {
		payload["replyTo"] = map[string]string{"email": msg.ReplyTo}
	}
	buf, _ := json.Marshal(payload)
	ctx, cancel := httpx.WithTimeout(ctx, c.Timeout)
	defer cancel()
	status, body, _, err := httpx.Do(ctx, c.HTTP, http.MethodPost, base+"/v3/smtp/email", map[string]string{
		"api-key":      c.APIKey,
		"Content-Type": "application/json",
	}, buf)
	if err != nil {
		return provider.Result{}, provider.Transient("brevo", err.Error(), 0)
	}
	if err := httpx.Classify("brevo", status, body); err != nil {
		return provider.Result{}, err
	}
	var out struct {
		MessageID string `json:"messageId"`
	}
	_ = json.Unmarshal(body, &out)
	return provider.Result{ProviderMessageID: out.MessageID}, nil
}

func emails(list []string) []map[string]string {
	out := make([]map[string]string, len(list))
	for i, addr := range list {
		out[i] = map[string]string{"email": addr}
	}
	return out
}
