package sendgrid

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"mcon/internal/message"
	"mcon/internal/provider"
)

func TestSendAndStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3/mail/send" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("request %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		w.Header().Set("X-Message-Id", "sg-1")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "secret", HTTP: srv.Client()}
	res, err := c.Send(context.Background(), message.Outbound{From: "a@b.c", To: []string{"c@d.e"}, Subject: "s", Text: "t"})
	if err != nil || res.ProviderMessageID != "sg-1" {
		t.Fatal(err, res)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadRequest)
	}))
	defer bad.Close()
	c.BaseURL = bad.URL
	_, err = c.Send(context.Background(), message.Outbound{From: "a@b.c", To: []string{"c@d.e"}, Subject: "s", Text: "t"})
	if !provider.IsPermanent(err) {
		t.Fatalf("got %v", err)
	}
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "later", http.StatusBadGateway)
	}))
	defer down.Close()
	c.BaseURL = down.URL
	_, err = c.Send(context.Background(), message.Outbound{From: "a@b.c", To: []string{"c@d.e"}, Subject: "s", Text: "t"})
	if !provider.IsTransient(err) {
		t.Fatalf("got %v", err)
	}
}
