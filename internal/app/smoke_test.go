package app

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"mcon/internal/config"
	"mcon/internal/ctl"
	"mcon/internal/version"
)

func TestSmokeSend(t *testing.T) {
	dir := t.TempDir()
	cfg, sec := config.Local(dir)
	cfg.Queue.PollInterval = config.Duration(20 * time.Millisecond)
	cfg.Queue.Workers = 1
	application, err := Open(cfg, sec, "", "")
	if err != nil {
		t.Fatal(err)
	}
	publicLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	adminLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- application.Serve(ctx, publicLn, adminLn) }()
	opt := ctl.Options{
		PublicURL:  "http://" + publicLn.Addr().String(),
		AdminURL:   "http://" + adminLn.Addr().String(),
		KeyID:      "website",
		HMACSecret: sec.Clients["website"].HMACSecret,
		AdminToken: sec.Admin.Token,
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := ctl.Health(ctx, opt); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("admin did not become ready")
		}
		time.Sleep(20 * time.Millisecond)
	}
	body := []byte(`{"from":"noreply@example.com","to":["user@example.com"],"subject":"Smoke","text":"hello from smoke"}`)
	raw, err := ctl.Send(ctx, opt, body, "smoke-key-1")
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	status, err := ctl.Wait(ctx, opt, idOf(raw), 5*time.Second)
	if err != nil || !strings.Contains(status, `"state":"sent"`) {
		cancel()
		t.Fatalf("status %s %v", status, err)
	}
	ver, err := ctl.Version(ctx, opt)
	if err != nil || ver != version.Version {
		cancel()
		t.Fatalf("version %s %v (want %s)", ver, err, version.Version)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown timed out")
	}
}

func idOf(raw string) string {
	i := strings.Index(raw, `"id":"`)
	if i < 0 {
		return ""
	}
	s := raw[i+6:]
	j := strings.IndexByte(s, '"')
	if j < 0 {
		return ""
	}
	return s[:j]
}
