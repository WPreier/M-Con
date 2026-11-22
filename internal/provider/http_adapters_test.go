package provider_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mcon/internal/message"
	"mcon/internal/provider"
	"mcon/internal/provider/brevo"
	"mcon/internal/provider/gmail"
	"mcon/internal/provider/graph"
	"mcon/internal/provider/mailgun"
	"mcon/internal/provider/postmark"
	"mcon/internal/provider/resend"
	"mcon/internal/provider/sendgrid"
	"mcon/internal/provider/ses"
)

func TestHTTPProviders(t *testing.T) {
	msg := message.Outbound{
		ID: "m1", From: "noreply@example.com", To: []string{"user@example.com"},
		Subject: "Hi", Text: "plain", HTML: "<b>x</b>", ReplyTo: "reply@example.com",
		Raw: []byte("Subject: Hi\r\n\r\nplain\r\n"), MessageID: "<m1@example.com>",
	}
	t.Run("sendgrid", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v3/mail/send" || r.Header.Get("Authorization") != "Bearer sg" {
				t.Errorf("path %s auth %s", r.URL.Path, r.Header.Get("Authorization"))
			}
			w.Header().Set("X-Message-Id", "sg-1")
			w.WriteHeader(http.StatusAccepted)
		}))
		defer srv.Close()
		res, err := (&sendgrid.Client{BaseURL: srv.URL, APIKey: "sg"}).Send(context.Background(), msg)
		if err != nil || res.ProviderMessageID != "sg-1" {
			t.Fatal(err, res)
		}
	})
	t.Run("mailgun", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(r.URL.Path, "/v3/mg.example.com/messages") {
				t.Errorf("path %s", r.URL.Path)
			}
			u, p, ok := r.BasicAuth()
			if !ok || u != "api" || p != "mg" {
				t.Errorf("auth %s %s", u, p)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "<mg>"})
		}))
		defer srv.Close()
		res, err := (&mailgun.Client{BaseURL: srv.URL, Domain: "mg.example.com", APIKey: "mg"}).Send(context.Background(), msg)
		if err != nil || res.ProviderMessageID != "<mg>" {
			t.Fatal(err, res)
		}
	})
	t.Run("postmark", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Postmark-Server-Token") != "pm" {
				t.Errorf("token %s", r.Header.Get("X-Postmark-Server-Token"))
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"MessageID": "pm-1"})
		}))
		defer srv.Close()
		res, err := (&postmark.Client{BaseURL: srv.URL, ServerToken: "pm"}).Send(context.Background(), msg)
		if err != nil || res.ProviderMessageID != "pm-1" {
			t.Fatal(err, res)
		}
	})
	t.Run("brevo", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("api-key") != "br" || r.URL.Path != "/v3/smtp/email" {
				t.Errorf("brevo %s %s", r.URL.Path, r.Header.Get("api-key"))
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"messageId": "br-1"})
		}))
		defer srv.Close()
		res, err := (&brevo.Client{BaseURL: srv.URL, APIKey: "br"}).Send(context.Background(), msg)
		if err != nil || res.ProviderMessageID != "br-1" {
			t.Fatal(err, res)
		}
	})
	t.Run("resend", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer re" {
				t.Errorf("auth %s", r.Header.Get("Authorization"))
			}
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = io.WriteString(w, "bad")
		}))
		defer srv.Close()
		_, err := (&resend.Client{BaseURL: srv.URL, APIKey: "re"}).Send(context.Background(), msg)
		if !provider.IsPermanent(err) {
			t.Fatal(err)
		}
	})
	t.Run("ses", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
				t.Errorf("auth %s", r.Header.Get("Authorization"))
			}
			if r.URL.Path != "/v2/email/outbound-emails" {
				t.Errorf("path %s", r.URL.Path)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"MessageId": "ses-1"})
		}))
		defer srv.Close()
		res, err := (&ses.Client{BaseURL: srv.URL, Region: "eu-central-1", AccessKey: "AKIA", SecretKey: "secret"}).Send(context.Background(), msg)
		if err != nil || res.ProviderMessageID != "ses-1" {
			t.Fatal(err, res)
		}
	})
	t.Run("graph", func(t *testing.T) {
		var srv *httptest.Server
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/token" {
				_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "expires_in": 3600})
				return
			}
			if r.Header.Get("Authorization") != "Bearer tok" || !strings.Contains(r.URL.Path, "/sendMail") {
				t.Errorf("graph %s %s", r.URL.Path, r.Header.Get("Authorization"))
			}
			w.WriteHeader(http.StatusAccepted)
		}))
		defer srv.Close()
		g := &graph.Client{BaseURL: srv.URL, TokenURL: srv.URL + "/token", TenantID: "t", ClientID: "c", ClientSecret: "s", SendAs: "sender@example.com"}
		if _, err := g.Send(context.Background(), msg); err != nil {
			t.Fatal(err)
		}
		if _, err := g.Send(context.Background(), msg); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("gmail", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "/token") || r.URL.Path == "/token" {
				_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "gtok", "expires_in": 3600})
				return
			}
			if r.Header.Get("Authorization") != "Bearer gtok" {
				t.Errorf("gmail auth %s path %s", r.Header.Get("Authorization"), r.URL.Path)
			}
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["raw"] == "" {
				t.Error("missing raw")
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "gm-1"})
		}))
		defer srv.Close()
		res, err := (&gmail.Client{
			BaseURL: srv.URL, TokenURL: srv.URL + "/token",
			ClientID: "c", ClientSecret: "s", RefreshToken: "r",
		}).Send(context.Background(), msg)
		if err != nil || res.ProviderMessageID != "gm-1" {
			t.Fatal(err, res)
		}
	})
}
