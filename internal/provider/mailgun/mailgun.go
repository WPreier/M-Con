// Package mailgun delivers mail through the Mailgun messages API.
package mailgun

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"mcon/internal/message"
	"mcon/internal/provider"
	"mcon/internal/provider/httpx"
)

// Client is a Mailgun adapter.
type Client struct {
	BaseURL string
	Domain  string
	APIKey  string
	Timeout time.Duration
	HTTP    *http.Client
}

// Send posts a multipart message.
func (c *Client) Send(ctx context.Context, msg message.Outbound) (provider.Result, error) {
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = "https://api.mailgun.net"
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("from", msg.From)
	_ = w.WriteField("subject", msg.Subject)
	for _, addr := range msg.To {
		_ = w.WriteField("to", addr)
	}
	for _, addr := range msg.Cc {
		_ = w.WriteField("cc", addr)
	}
	for _, addr := range msg.Bcc {
		_ = w.WriteField("bcc", addr)
	}
	if msg.Text != "" {
		_ = w.WriteField("text", msg.Text)
	}
	if msg.HTML != "" {
		_ = w.WriteField("html", msg.HTML)
	}
	if msg.ReplyTo != "" {
		_ = w.WriteField("h:Reply-To", msg.ReplyTo)
	}
	for _, att := range msg.Attachments {
		part, err := w.CreateFormFile("attachment", att.Filename)
		if err != nil {
			return provider.Result{}, provider.Permanent("mailgun", err.Error(), 0)
		}
		if _, err := part.Write(att.Data); err != nil {
			return provider.Result{}, provider.Permanent("mailgun", err.Error(), 0)
		}
	}
	_ = w.Close()
	ctx, cancel := httpx.WithTimeout(ctx, c.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v3/"+c.Domain+"/messages", &buf)
	if err != nil {
		return provider.Result{}, provider.Permanent("mailgun", err.Error(), 0)
	}
	req.SetBasicAuth("api", c.APIKey)
	req.Header.Set("Content-Type", w.FormDataContentType())
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		return provider.Result{}, provider.Transient("mailgun", err.Error(), 0)
	}
	defer res.Body.Close()
	var payload struct {
		ID string `json:"id"`
	}
	body := make([]byte, 0)
	tmp := make([]byte, 1024)
	for {
		n, rerr := res.Body.Read(tmp)
		body = append(body, tmp[:n]...)
		if rerr != nil {
			break
		}
	}
	if err := httpx.Classify("mailgun", res.StatusCode, body); err != nil {
		return provider.Result{}, err
	}
	_ = json.Unmarshal(body, &payload)
	return provider.Result{ProviderMessageID: payload.ID}, nil
}
