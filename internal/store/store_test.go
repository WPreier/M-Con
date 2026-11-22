package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenWALAndMigrations(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	mode, err := s.JournalMode(ctx)
	if err != nil || mode != "wal" {
		t.Fatalf("journal %q %v", mode, err)
	}
	syncMode, err := s.Synchronous(ctx)
	if err != nil || syncMode != 2 {
		t.Fatalf("synchronous %d %v", syncMode, err)
	}
	fk, err := s.ForeignKeys(ctx)
	if err != nil || fk != 1 {
		t.Fatalf("foreign_keys %d %v", fk, err)
	}
	applied, err := s.AppliedMigrations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 2 || applied[0] != 1 || applied[1] != 2 {
		t.Fatalf("migrations %#v", applied)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	applied, _ = s.AppliedMigrations(ctx)
	if len(applied) != 2 {
		t.Fatalf("reapplied %#v", applied)
	}
}

func TestMessageClaimAndNonce(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	msg := sample(now)
	if err := s.InsertNew(ctx, msg, &Idempotency{
		ClientKey: "website", Key: "k1", RequestHash: "abc", MessageID: msg.ID,
		ResponseJSON: `{"id":"1"}`, CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, msg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "received" || len(got.Recipients) != 2 || len(got.Attachments) != 1 || string(got.Attachments[0].Data) != "hi" {
		t.Fatalf("roundtrip %+v", got)
	}
	if err := s.InsertNew(ctx, sample(now), &Idempotency{
		ClientKey: "website", Key: "k1", RequestHash: "other", MessageID: "x",
		ResponseJSON: `{}`, CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}); err != ErrConflict {
		t.Fatalf("conflict %v", err)
	}
	claimed, err := s.Claim(ctx, "w1", time.Minute, 10, now)
	if err != nil || len(claimed) != 1 || claimed[0].State != "spam_check" {
		t.Fatalf("claim %#v %v", claimed, err)
	}
	again, err := s.Claim(ctx, "w2", time.Minute, 10, now)
	if err != nil || len(again) != 0 {
		t.Fatalf("second claim %#v %v", again, err)
	}
	if err := s.DebugExec(ctx, `UPDATE messages SET lease_until=? WHERE id=?`, FormatTime(now.Add(-time.Second)), msg.ID); err != nil {
		t.Fatal(err)
	}
	reclaimed, err := s.Claim(ctx, "w2", time.Minute, 10, now)
	if err != nil || len(reclaimed) != 1 || reclaimed[0].LeasedBy != "w2" {
		t.Fatalf("reclaim %#v %v", reclaimed, err)
	}
	exp := time.Now().Add(time.Minute)
	if err := s.UseNonce(ctx, "website", "nonce-1", exp); err != nil {
		t.Fatal(err)
	}
	if err := s.UseNonce(ctx, "website", "nonce-1", exp); err != ErrReplay {
		t.Fatalf("replay %v", err)
	}
	if err := s.Backup(ctx, filepath.Join(t.TempDir(), "bak.db")); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationsMatchTree(t *testing.T) {
	root := moduleRoot(t)
	for _, name := range []string{"001_init.sql", "002_messages_updated_idx.sql"} {
		a, err := os.ReadFile(filepath.Join(root, "db", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(root, "internal", "store", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if string(a) != string(b) {
			t.Fatalf("%s differs between db/migrations and embedded copy", name)
		}
	}
}

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "mcon.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func sample(now time.Time) *Message {
	return &Message{
		ID: "01TESTMESSAGEID0000000000", ClientKey: "website", State: "received", Provider: "discard",
		From: "noreply@example.com", Subject: "Hello", Text: "body",
		Headers: map[string]string{}, Tags: []string{"contact"}, Metadata: map[string]any{"form": "contact"},
		ContentSHA256: "abc", BodySHA256: "def", RFCMessageID: "<id@example.com>",
		MaxAttempts: 3, CreatedAt: now, UpdatedAt: now,
		Recipients:  []Recipient{{Kind: "to", Address: "a@b.c"}, {Kind: "cc", Address: "c@d.e"}},
		Attachments: []Attachment{{Filename: "a.txt", ContentType: "text/plain", SHA256: "zz", Size: 2, Data: []byte("hi")}},
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
