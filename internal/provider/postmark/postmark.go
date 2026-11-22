// Package postmark delivers mail through the Postmark API.
package postmark

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

// Client is a Postmark adapter.
type Client struct {
	BaseURL     string
	ServerToken string
	Timeout     time.Duration
	HTTP        *http.Client
}

// Send posts one email.
func (c *Client) Send(ctx context.Context, msg message.Outbound) (provider.Result, error) {
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = "https://api.postmarkapp.com"
	}
	payload := map[string]any{
		"From": msg.From, "To": strings.Join(msg.To, ","), "Subject": msg.Subject,
		"TextBody": msg.Text, "HtmlBody": msg.HTML, "MessageStream": "outbound",
	}
	if len(msg.Cc) > 0 {
		payload["Cc"] = strings.Join(msg.Cc, ",")
	}
	if len(msg.Bcc) > 0 {
		payload["Bcc"] = strings.Join(msg.Bcc, ",")
	}
	if msg.ReplyTo != "" {
		payload["ReplyTo"] = msg.ReplyTo
	}
	buf, _ := json.Marshal(payload)
	ctx, cancel := httpx.WithTimeout(ctx, c.Timeout)
	defer cancel()
	status, body, _, err := httpx.Do(ctx, c.HTTP, http.MethodPost, base+"/email", map[string]string{
		"X-Postmark-Server-Token": c.ServerToken,
		"Accept":                  "application/json",
		"Content-Type":            "application/json",
	}, buf)
	if err != nil {
		return provider.Result{}, provider.Transient("postmark", err.Error(), 0)
	}
	if err := httpx.Classify("postmark", status, body); err != nil {
		return provider.Result{}, err
	}
	var out struct {
		MessageID string `json:"MessageID"`
	}
	_ = json.Unmarshal(body, &out)
	return provider.Result{ProviderMessageID: out.MessageID}, nil
}
