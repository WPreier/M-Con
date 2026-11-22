package brevo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"mcon/internal/message"
)

func TestSend(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("api-key") != "k" || r.URL.Path != "/v3/smtp/email" {
			t.Errorf("bad request")
		}
		_, _ = w.Write([]byte(`{"messageId":"<br-1>"}`))
	}))
	defer srv.Close()
	res, err := (&Client{BaseURL: srv.URL, APIKey: "k", HTTP: srv.Client()}).Send(context.Background(), message.Outbound{From: "a@b.c", To: []string{"c@d.e"}, Subject: "s", Text: "t"})
	if err != nil || res.ProviderMessageID != "<br-1>" {
		t.Fatal(err, res)
	}
}
