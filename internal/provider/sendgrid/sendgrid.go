// Package sendgrid delivers mail through the SendGrid v3 API.
package sendgrid

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"mcon/internal/message"
	"mcon/internal/provider"
	"mcon/internal/provider/httpx"
)

// Client is a SendGrid adapter.
type Client struct {
	BaseURL string
	APIKey  string
	Timeout time.Duration
	HTTP    *http.Client
}

type address struct {
	Email string `json:"email"`
}

// Send posts a JSON mail payload.
func (c *Client) Send(ctx context.Context, msg message.Outbound) (provider.Result, error) {
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = "https://api.sendgrid.com"
	}
	personal := map[string]any{"to": emails(msg.To)}
	if len(msg.Cc) > 0 {
		personal["cc"] = emails(msg.Cc)
	}
	if len(msg.Bcc) > 0 {
		personal["bcc"] = emails(msg.Bcc)
	}
	payload := map[string]any{
		"personalizations": []any{personal},
		"from":             address{Email: msg.From},
		"subject":          msg.Subject,
	}
	if msg.ReplyTo != "" {
		payload["reply_to"] = address{Email: msg.ReplyTo}
	}
	var content []map[string]string
	if msg.Text != "" {
		content = append(content, map[string]string{"type": "text/plain", "value": msg.Text})
	}
	if msg.HTML != "" {
		content = append(content, map[string]string{"type": "text/html", "value": msg.HTML})
	}
	payload["content"] = content
	if len(msg.Attachments) > 0 {
		var atts []map[string]string
		for _, att := range msg.Attachments {
			ct := att.ContentType
			if ct == "" {
				ct = "application/octet-stream"
			}
			atts = append(atts, map[string]string{
				"content":  base64.StdEncoding.EncodeToString(att.Data),
				"filename": att.Filename,
				"type":     ct,
			})
		}
		payload["attachments"] = atts
	}
	buf, _ := json.Marshal(payload)
	ctx, cancel := httpx.WithTimeout(ctx, c.Timeout)
	defer cancel()
	status, body, hdr, err := httpx.Do(ctx, c.HTTP, http.MethodPost, base+"/v3/mail/send", map[string]string{
		"Authorization": "Bearer " + c.APIKey,
		"Content-Type":  "application/json",
	}, buf)
	if err != nil {
		return provider.Result{}, provider.Transient("sendgrid", err.Error(), 0)
	}
	if err := httpx.Classify("sendgrid", status, body); err != nil {
		return provider.Result{}, err
	}
	return provider.Result{ProviderMessageID: hdr.Get("X-Message-Id")}, nil
}

func emails(list []string) []address {
	out := make([]address, len(list))
	for i, addr := range list {
		out[i] = address{Email: addr}
	}
	return out
}
