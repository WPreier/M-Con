package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalValidates(t *testing.T) {
	dir := t.TempDir()
	cfg, sec := Local(dir)
	if err := Validate(cfg, sec); err != nil {
		t.Fatal(err)
	}
}

func TestPlaintextSMTPOnlyLoopback(t *testing.T) {
	dir := t.TempDir()
	cfg, sec := Local(dir)
	cfg.Providers.Items["remote"] = Provider{
		Type: "smtp", Host: "203.0.113.10", Port: 25, TLSMode: "none", Timeout: cfg.Queue.Lease,
	}
	err := Validate(cfg, sec)
	if err == nil || !strings.Contains(err.Error(), "plaintext SMTP") {
		t.Fatalf("expected plaintext rejection, got %v", err)
	}

	cfg, sec = Local(dir)
	cfg.Providers.Items["local-smtp"] = Provider{
		Type: "smtp", Host: "127.0.0.1", Port: 25, TLSMode: "none", Timeout: cfg.Queue.Lease,
	}
	if err := Validate(cfg, sec); err != nil {
		t.Fatal(err)
	}
}

func TestRejectsBadTimeoutPolicy(t *testing.T) {
	dir := t.TempDir()
	cfg, sec := Local(dir)
	cfg.Spam.OnTimeout = "drop"
	if err := Validate(cfg, sec); err == nil || !strings.Contains(err.Error(), "on_timeout") {
		t.Fatalf("got %v", err)
	}
}

func TestPublicTLSRequiredOffLoopback(t *testing.T) {
	dir := t.TempDir()
	cfg, sec := Local(dir)
	cfg.Server.PublicAddr = ":8687"
	cfg.Server.TLS.Enabled = false
	if err := Validate(cfg, sec); err == nil || !strings.Contains(err.Error(), "tls") {
		t.Fatalf("got %v", err)
	}
}

func TestExampleFilesValidate(t *testing.T) {
	root := moduleRoot(t)
	cfg, err := Load(filepath.Join(root, "configs", "mcon.yaml.example"))
	if err != nil {
		t.Fatal(err)
	}
	sec, err := LoadSecrets(filepath.Join(root, "configs", "secrets.yaml.example"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(cfg, sec); err != nil {
		t.Fatal(err)
	}
}

func TestMissingSecretRejected(t *testing.T) {
	dir := t.TempDir()
	cfg, sec := Local(dir)
	delete(sec.Clients, "website")
	if err := Validate(cfg, sec); err == nil {
		t.Fatal("expected missing hmac secret")
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
