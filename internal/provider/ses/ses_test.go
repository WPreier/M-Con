package ses

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mcon/internal/message"
)

func TestKnownVector(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://example.amazonaws.com/?Param2=value2&Param1=value1", nil)
	if err != nil {
		t.Fatal(err)
	}
	// The published example signs host and x-amz-date only. Recompute that shape here.
	now := time.Date(2015, 8, 30, 12, 36, 0, 0, time.UTC)
	payloadHash := hex.EncodeToString(func() []byte { sum := sha256.Sum256(nil); return sum[:] }())
	canon := "GET\n/\nParam1=value1&Param2=value2\nhost:example.amazonaws.com\nx-amz-date:20150830T123600Z\n\nhost;x-amz-date\n" + payloadHash
	sum := sha256.Sum256([]byte(canon))
	stringToSign := "AWS4-HMAC-SHA256\n20150830T123600Z\n20150830/us-east-1/service/aws4_request\n" + hex.EncodeToString(sum[:])
	sig := hex.EncodeToString(hmacSHA(signingKey("wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY", "20150830", "us-east-1", "service"), []byte(stringToSign)))
	if sig != "b97d918cfa904a5beff61c982a1b6f458b799221646efd99d3219ec94cdf2500" {
		t.Fatalf("signature %s\ncanon hash %s", sig, hex.EncodeToString(sum[:]))
	}
	_ = req
	_ = now
}

func TestSendSignsRequest(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if r.URL.Path != "/v2/email/outbound-emails" {
			t.Errorf("path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"MessageId":"ses-1"}`))
	}))
	defer srv.Close()
	c := &Client{
		BaseURL: srv.URL, Region: "eu-central-1",
		AccessKey: "AKIDEXAMPLE", SecretKey: "secret", HTTP: srv.Client(),
		Now: func() time.Time { return time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC) },
	}
	res, err := c.Send(context.Background(), message.Outbound{From: "a@b.c", To: []string{"c@d.e"}, Raw: []byte("Subject: s\r\n\r\nhi\r\n")})
	if err != nil || res.ProviderMessageID != "ses-1" {
		t.Fatal(err, res)
	}
	if len(auth) < 20 || auth[:16] != "AWS4-HMAC-SHA256" {
		t.Fatalf("auth %s", auth)
	}
}
