package logging

import (
	"bytes"
	"strings"
	"testing"
)

func TestRedactsSecrets(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, "debug", nil)
	log.Info("accepted", "message_id", "abc", "hmac_secret", "super-secret", "token", "abc")
	out := buf.String()
	if strings.Contains(out, "super-secret") || strings.Contains(out, `"token":"abc"`) {
		t.Fatalf("secret leaked: %s", out)
	}
	if !strings.Contains(out, "[redacted]") || !strings.Contains(out, "abc") {
		t.Fatalf("log missing fields: %s", out)
	}
}

func TestLevelFilter(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, "warn", nil)
	log.Info("hidden")
	log.Warn("shown")
	if strings.Contains(buf.String(), "hidden") || !strings.Contains(buf.String(), "shown") {
		t.Fatalf("filter failed: %s", buf.String())
	}
}
