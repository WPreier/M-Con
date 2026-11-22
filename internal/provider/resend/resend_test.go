package resend

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"mcon/internal/message"
)

func TestSend(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer rk" || r.URL.Path != "/emails" {
			t.Errorf("bad request")
		}
		_, _ = w.Write([]byte(`{"id":"re_1"}`))
	}))
	defer srv.Close()
	res, err := (&Client{BaseURL: srv.URL, APIKey: "rk", HTTP: srv.Client()}).Send(context.Background(), message.Outbound{From: "a@b.c", To: []string{"c@d.e"}, Subject: "s", Text: "t"})
	if err != nil || res.ProviderMessageID != "re_1" {
		t.Fatal(err, res)
	}
}
