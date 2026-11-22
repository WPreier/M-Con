package queue

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"mcon/internal/config"
	"mcon/internal/logging"
	"mcon/internal/provider"
	"mcon/internal/provider/discard"
	"mcon/internal/store"
)

func TestDiscardDLQAndReplay(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "mcon.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cfg, _ := config.Local(dir)
	cfg.Queue.BackoffInitial = config.Duration(time.Millisecond)
	cfg.Queue.BackoffMax = config.Duration(time.Second)
	cfg.Queue.Workers = 4
	d := &discard.Provider{Mode: "transient", FailTimes: 2}
	reg := provider.NewRegistry(5, time.Minute)
	reg.Set("discard", d, "")
	m := &Manager{
		Store: st, Reg: func() *provider.Registry { return reg }, Log: logging.Nop(), ID: "t",
		Cfg: func() config.Snapshot { return config.Snapshot{Config: cfg} },
	}
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	msg := &store.Message{
		ID: "01DLQMESSAGEID000000000000", ClientKey: "website", State: "received", Provider: "discard",
		From: "noreply@example.com", Subject: "Hi", Text: "body", RFCMessageID: "<id@example.com>",
		MaxAttempts: 2, SkipSpam: true, CreatedAt: now, UpdatedAt: now,
		Headers: map[string]string{}, Tags: []string{}, Metadata: map[string]any{},
		Recipients: []store.Recipient{{Kind: "to", Address: "a@b.c"}},
	}
	if err := st.InsertNew(context.Background(), msg, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Tick(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetMeta(context.Background(), msg.ID)
	if got.State != "retry_wait" || got.Attempts != 1 {
		t.Fatalf("after first %+v", got.State)
	}
	if n, err := m.Tick(context.Background(), now.Add(time.Millisecond)); err != nil || n != 1 {
		t.Fatalf("second tick %d %v", n, err)
	}
	got, _ = st.GetMeta(context.Background(), msg.ID)
	if got.State != "dead" {
		t.Fatalf("state %s err %s", got.State, got.LastError)
	}
	if err := st.Replay(context.Background(), msg.ID, now); err != nil {
		t.Fatal(err)
	}
	d.Mode = "success"
	d.FailTimes = 0
	if _, err := m.Tick(context.Background(), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetMeta(context.Background(), msg.ID)
	if got.State != "sent" || got.ProviderMessageID == "" {
		t.Fatalf("replay state %s id %s", got.State, got.ProviderMessageID)
	}
}
