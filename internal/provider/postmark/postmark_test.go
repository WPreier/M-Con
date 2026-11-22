package postmark

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"mcon/internal/message"
)

func TestSend(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Postmark-Server-Token") != "tok" || r.URL.Path != "/email" {
			t.Errorf("bad request")
		}
		_, _ = w.Write([]byte(`{"MessageID":"pm-1","ErrorCode":0}`))
	}))
	defer srv.Close()
	res, err := (&Client{BaseURL: srv.URL, ServerToken: "tok", HTTP: srv.Client()}).Send(context.Background(), message.Outbound{From: "a@b.c", To: []string{"c@d.e"}, Subject: "s", Text: "t"})
	if err != nil || res.ProviderMessageID != "pm-1" {
		t.Fatal(err, res)
	}
}
