// Package ses delivers raw RFC822 mail through Amazon SES v2.
package ses

import (
	"bytes"
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

// Client is an SES adapter.
type Client struct {
	BaseURL   string
	Region    string
	AccessKey string
	SecretKey string
	Timeout   time.Duration
	HTTP      *http.Client
	Now       func() time.Time
}

// Send posts a signed SendEmail request with a raw MIME body.
func (c *Client) Send(ctx context.Context, msg message.Outbound) (provider.Result, error) {
	region := c.Region
	if region == "" {
		region = "eu-central-1"
	}
	endpoint := strings.TrimRight(c.BaseURL, "/")
	if endpoint == "" {
		endpoint = "https://email." + region + ".amazonaws.com"
	}
	if !strings.Contains(endpoint, "/v2/email/outbound-emails") {
		endpoint += "/v2/email/outbound-emails"
	}
	raw := msg.Raw
	if len(raw) == 0 {
		return provider.Result{}, provider.Permanent("ses", "missing rfc822 body", 0)
	}
	payload := map[string]any{
		"FromEmailAddress": msg.From,
		"Destination": map[string]any{
			"ToAddresses":  msg.To,
			"CcAddresses":  msg.Cc,
			"BccAddresses": msg.Bcc,
		},
		"Content": map[string]any{"Raw": map[string]string{"Data": base64.StdEncoding.EncodeToString(raw)}},
	}
	buf, _ := json.Marshal(payload)
	ctx, cancel := httpx.WithTimeout(ctx, c.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(buf))
	if err != nil {
		return provider.Result{}, provider.Permanent("ses", err.Error(), 0)
	}
	req.Header.Set("Content-Type", "application/json")
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	SignV4(req, buf, region, "ses", c.AccessKey, c.SecretKey, now)
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		return provider.Result{}, provider.Transient("ses", err.Error(), 0)
	}
	defer res.Body.Close()
	body := make([]byte, 0)
	tmp := make([]byte, 2048)
	for {
		n, rerr := res.Body.Read(tmp)
		body = append(body, tmp[:n]...)
		if rerr != nil {
			break
		}
	}
	if err := httpx.Classify("ses", res.StatusCode, body); err != nil {
		return provider.Result{}, err
	}
	var out struct {
		MessageID string `json:"MessageId"`
	}
	_ = json.Unmarshal(body, &out)
	return provider.Result{ProviderMessageID: out.MessageID}, nil
}
