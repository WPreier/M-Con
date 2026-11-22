package mailgun

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"mcon/internal/message"
)

func TestSend(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "api" || pass != "key" || r.URL.Path != "/v3/mg.example.com/messages" {
			t.Errorf("auth/path %s %v", r.URL.Path, ok)
		}
		if _, err := io.ReadAll(r.Body); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"<mg-1>","message":"Queued"}`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Domain: "mg.example.com", APIKey: "key", HTTP: srv.Client()}
	res, err := c.Send(context.Background(), message.Outbound{From: "a@b.c", To: []string{"c@d.e"}, Subject: "s", Text: "t"})
	if err != nil || res.ProviderMessageID != "<mg-1>" {
		t.Fatal(err, res)
	}
}
