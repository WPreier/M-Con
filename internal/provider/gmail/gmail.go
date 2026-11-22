// Package gmail delivers raw RFC822 mail through the Gmail API.
package gmail

import (
	"context"
	"encoding/base64"
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

// Client is a Gmail API adapter.
type Client struct {
	BaseURL      string
	TokenURL     string
	ClientID     string
	ClientSecret string
	RefreshToken string
	Timeout      time.Duration
	HTTP         *http.Client
	mu           sync.Mutex
	token        string
	exp          time.Time
}

// Send refreshes an access token and posts the raw message.
func (c *Client) Send(ctx context.Context, msg message.Outbound) (provider.Result, error) {
	token, err := c.accessToken(ctx)
	if err != nil {
		return provider.Result{}, err
	}
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = "https://gmail.googleapis.com"
	}
	raw := msg.Raw
	if len(raw) == 0 {
		return provider.Result{}, provider.Permanent("gmail", "missing rfc822 body", 0)
	}
	buf, _ := json.Marshal(map[string]string{"raw": base64.RawURLEncoding.EncodeToString(raw)})
	ctx, cancel := httpx.WithTimeout(ctx, c.Timeout)
	defer cancel()
	status, body, _, err := httpx.Do(ctx, c.HTTP, http.MethodPost, base+"/gmail/v1/users/me/messages/send", map[string]string{
		"Authorization": "Bearer " + token,
		"Content-Type":  "application/json",
	}, buf)
	if err != nil {
		return provider.Result{}, provider.Transient("gmail", err.Error(), 0)
	}
	if err := httpx.Classify("gmail", status, body); err != nil {
		return provider.Result{}, err
	}
	var out struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(body, &out)
	return provider.Result{ProviderMessageID: out.ID}, nil
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
		endpoint = "https://oauth2.googleapis.com/token"
	}
	form := url.Values{}
	form.Set("client_id", c.ClientID)
	form.Set("client_secret", c.ClientSecret)
	form.Set("refresh_token", c.RefreshToken)
	form.Set("grant_type", "refresh_token")
	ctx, cancel := httpx.WithTimeout(ctx, c.Timeout)
	defer cancel()
	status, body, _, err := httpx.Do(ctx, c.HTTP, http.MethodPost, endpoint, map[string]string{
		"Content-Type": "application/x-www-form-urlencoded",
	}, []byte(form.Encode()))
	if err != nil {
		return "", provider.Transient("gmail", err.Error(), 0)
	}
	if err := httpx.Classify("gmail", status, body); err != nil {
		return "", err
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.AccessToken == "" {
		return "", provider.Transient("gmail", "token response", status)
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
