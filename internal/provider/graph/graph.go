// Package graph delivers mail through Microsoft Graph.
package graph

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"mcon/internal/message"
	"mcon/internal/provider"
	"mcon/internal/provider/httpx"
)

// Client is a Microsoft Graph sendMail adapter.
type Client struct {
	BaseURL      string
	TokenURL     string
	TenantID     string
	ClientID     string
	ClientSecret string
	SendAs       string
	Timeout      time.Duration
	HTTP         *http.Client
	mu           sync.Mutex
	token        string
	exp          time.Time
}

// Send obtains an app token and posts sendMail.
func (c *Client) Send(ctx context.Context, msg message.Outbound) (provider.Result, error) {
	token, err := c.accessToken(ctx)
	if err != nil {
		return provider.Result{}, err
	}
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = "https://graph.microsoft.com/v1.0"
	}
	contentType := "Text"
	content := msg.Text
	if msg.HTML != "" {
		contentType = "HTML"
		content = msg.HTML
	}
	payload := map[string]any{
		"message": map[string]any{
			"subject":       msg.Subject,
			"body":          map[string]string{"contentType": contentType, "content": content},
			"toRecipients":  recips(msg.To),
			"ccRecipients":  recips(msg.Cc),
			"bccRecipients": recips(msg.Bcc),
		},
		"saveToSentItems": false,
	}
	if msg.ReplyTo != "" {
		payload["message"].(map[string]any)["replyTo"] = recips([]string{msg.ReplyTo})
	}
	buf, _ := json.Marshal(payload)
	ctx, cancel := httpx.WithTimeout(ctx, c.Timeout)
	defer cancel()
	sendAs := url.PathEscape(c.SendAs)
	status, body, _, err := httpx.Do(ctx, c.HTTP, http.MethodPost, base+"/users/"+sendAs+"/sendMail", map[string]string{
		"Authorization": "Bearer " + token,
		"Content-Type":  "application/json",
	}, buf)
	if err != nil {
		return provider.Result{}, provider.Transient("graph", err.Error(), 0)
	}
	if err := httpx.Classify("graph", status, body); err != nil {
		return provider.Result{}, err
	}
	return provider.Result{ProviderMessageID: msg.ID}, nil
}

func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.token != "" && time.Now().Before(c.exp) {
		tok := c.token
		c.mu.Unlock()
		return tok, nil
	}
	c.mu.Unlock()
	endpoint := c.TokenURL
	if endpoint == "" {
		endpoint = "https://login.microsoftonline.com/" + url.PathEscape(c.TenantID) + "/oauth2/v2.0/token"
	}
	form := url.Values{}
	form.Set("client_id", c.ClientID)
	form.Set("client_secret", c.ClientSecret)
	form.Set("scope", "https://graph.microsoft.com/.default")
	form.Set("grant_type", "client_credentials")
	ctx, cancel := httpx.WithTimeout(ctx, c.Timeout)
	defer cancel()
	status, body, _, err := httpx.Do(ctx, c.HTTP, http.MethodPost, endpoint, map[string]string{
		"Content-Type": "application/x-www-form-urlencoded",
	}, []byte(form.Encode()))
	if err != nil {
		return "", provider.Transient("graph", err.Error(), 0)
	}
	if err := httpx.Classify("graph", status, body); err != nil {
		return "", err
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.AccessToken == "" {
		return "", provider.Transient("graph", "token response", status)
	}
	if out.ExpiresIn <= 0 {
		out.ExpiresIn = 3600
	}
	c.mu.Lock()
	c.token = out.AccessToken
	c.exp = time.Now().Add(time.Duration(out.ExpiresIn)*time.Second - 30*time.Second)
	c.mu.Unlock()
	return out.AccessToken, nil
}

func recips(list []string) []map[string]any {
	out := make([]map[string]any, 0, len(list))
	for _, addr := range list {
		out = append(out, map[string]any{"emailAddress": map[string]string{"address": addr}})
	}
	return out
}
