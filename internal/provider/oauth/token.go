// Package oauth fetches access tokens for Graph and Gmail.
package oauth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"mcon/internal/provider"
)

// Token is an access token and its expiry.
type Token struct {
	Access    string
	ExpiresAt time.Time
}

// ClientCredentials performs the OAuth2 client-credentials grant.
func ClientCredentials(ctx context.Context, client *http.Client, tokenURL, clientID, secret, scope string) (Token, error) {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", clientID)
	form.Set("client_secret", secret)
	form.Set("scope", scope)
	return post(ctx, client, tokenURL, form)
}

// Refresh exchanges a refresh token.
func Refresh(ctx context.Context, client *http.Client, tokenURL, clientID, secret, refresh string) (Token, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refresh)
	form.Set("client_id", clientID)
	form.Set("client_secret", secret)
	return post(ctx, client, tokenURL, form)
}

func post(ctx context.Context, client *http.Client, tokenURL string, form url.Values) (Token, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return Token{}, provider.Transient("oauth", err.Error(), 0)
	}
	defer resp.Body.Close()
	buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			return Token{}, provider.Transient("oauth", string(buf), resp.StatusCode)
		}
		return Token{}, provider.Permanent("oauth", string(buf), resp.StatusCode)
	}
	var parsed struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(buf, &parsed); err != nil || parsed.AccessToken == "" {
		return Token{}, provider.Transient("oauth", "token response missing access_token", 0)
	}
	exp := time.Now().Add(time.Hour)
	if parsed.ExpiresIn > 0 {
		exp = time.Now().Add(time.Duration(parsed.ExpiresIn) * time.Second)
	}
	return Token{Access: parsed.AccessToken, ExpiresAt: exp}, nil
}
