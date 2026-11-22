package gmail

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"mcon/internal/message"
)

func TestSendRaw(t *testing.T) {
	var raw string
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"g","expires_in":3600}`))
	})
	mux.HandleFunc("/gmail/v1/users/me/messages/send", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Raw string `json:"raw"`
		}
		buf, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(buf, &body)
		decoded, _ := base64.RawURLEncoding.DecodeString(body.Raw)
		raw = string(decoded)
		_, _ = w.Write([]byte(`{"id":"g-1"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, TokenURL: srv.URL + "/token", ClientID: "id", ClientSecret: "sec", RefreshToken: "ref", HTTP: srv.Client()}
	res, err := c.Send(context.Background(), message.Outbound{Raw: []byte("Subject: hi\r\n\r\nbody\r\n")})
	if err != nil || res.ProviderMessageID != "g-1" || !stringsContains(raw, "Subject: hi") {
		t.Fatal(err, res, raw)
	}
}

func stringsContains(s, part string) bool {
	return len(s) >= len(part) && (s == part || len(part) == 0 || (len(s) > 0 && (func() bool { return stringIndex(s, part) >= 0 })()))
}

func stringIndex(s, part string) int {
	for i := 0; i+len(part) <= len(s); i++ {
		if s[i:i+len(part)] == part {
			return i
		}
	}
	return -1
}
