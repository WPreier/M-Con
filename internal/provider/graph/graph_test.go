package graph

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"mcon/internal/message"
)

func TestSendCachesToken(t *testing.T) {
	var tokens atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		tokens.Add(1)
		_, _ = w.Write([]byte(`{"access_token":"tok","expires_in":3600}`))
	})
	mux.HandleFunc("/v1.0/users/sender@example.com/sendMail", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("auth %s", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusAccepted)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := &Client{BaseURL: srv.URL + "/v1.0", TokenURL: srv.URL + "/token", SendAs: "sender@example.com", ClientID: "id", ClientSecret: "sec", HTTP: srv.Client()}
	msg := message.Outbound{ID: "m1", From: "sender@example.com", To: []string{"a@b.c"}, Subject: "s", HTML: "<b>x</b>"}
	if _, err := c.Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if tokens.Load() != 1 {
		t.Fatalf("token calls %d", tokens.Load())
	}
}
