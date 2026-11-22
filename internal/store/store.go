// Package store persists the queue, archive index, and idempotency records in SQLite.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

const timeLayout = "2006-01-02T15:04:05.000000000Z"

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned when an idempotency key was reused with a different body.
var ErrConflict = errors.New("conflict")

// ErrState is returned when the message state does not allow the transition.
var ErrState = errors.New("invalid state")

// ErrReplay is returned when a nonce was already used.
var ErrReplay = errors.New("nonce replay")

// Store is a SQLite database opened in WAL mode with synchronous=FULL.
type Store struct {
	db *sql.DB
}

// Open creates the database file, enables WAL, and applies migrations.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	var mode string
	if err := db.QueryRow(`PRAGMA journal_mode=WAL`).Scan(&mode); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA synchronous=FULL`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA busy_timeout=5000`); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &Store{db: db}
	if err := s.Migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Ping checks the connection.
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// DB exposes the handle for tests in this package.
func (s *Store) DB() *sql.DB { return s.db }

func dsn(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs), OmitHost: true}
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(FULL)")
	q.Add("_pragma", "foreign_keys(ON)")
	q.Set("_txlock", "immediate")
	u.RawQuery = q.Encode()
	return u.String()
}

// Migrate applies embedded SQL files in order.
func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return err
	}
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		ver, err := migrationVersion(entry.Name())
		if err != nil {
			return err
		}
		var n int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, ver).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		body, err := migrationFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return err
		}
		for _, stmt := range splitSQL(string(body)) {
			if _, err := s.db.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("%s: %w", entry.Name(), err)
			}
		}
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO schema_migrations(version, name, applied_at) VALUES(?, ?, ?)`,
			ver, entry.Name(), FormatTime(time.Now().UTC()),
		); err != nil {
			return err
		}
	}
	return nil
}

func migrationVersion(name string) (int, error) {
	prefix := name
	if i := strings.IndexByte(name, '_'); i > 0 {
		prefix = name[:i]
	}
	n, err := strconv.Atoi(prefix)
	if err != nil {
		return 0, fmt.Errorf("migration name %s", name)
	}
	return n, nil
}

func splitSQL(script string) []string {
	var b strings.Builder
	for _, line := range strings.Split(script, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	var out []string
	for _, part := range strings.Split(b.String(), ";") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// JournalMode returns the SQLite journal mode, expected "wal".
func (s *Store) JournalMode(ctx context.Context) (string, error) {
	var mode string
	err := s.db.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&mode)
	return strings.ToLower(mode), err
}

// Synchronous returns the SQLite synchronous pragma. 2 is FULL.
func (s *Store) Synchronous(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `PRAGMA synchronous`).Scan(&n)
	return n, err
}

// Backup writes a consistent snapshot with VACUUM INTO.
func (s *Store) Backup(ctx context.Context, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	_ = os.Remove(dest)
	escaped := strings.ReplaceAll(filepath.ToSlash(dest), "'", "''")
	_, err := s.db.ExecContext(ctx, fmt.Sprintf("VACUUM INTO '%s'", escaped))
	return err
}

// Cleanup drops expired nonces and idempotency keys.
func (s *Store) Cleanup(ctx context.Context, now time.Time) error {
	ts := FormatTime(now.UTC())
	if _, err := s.db.ExecContext(ctx, `DELETE FROM nonces WHERE expires_at <= ?`, ts); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM idempotency WHERE expires_at <= ?`, ts)
	return err
}

// FormatTime formats a UTC timestamp that sorts lexicographically.
func FormatTime(t time.Time) string {
	return t.UTC().Format(timeLayout)
}

// ParseTime parses a timestamp written by FormatTime.
func ParseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, errors.New("empty time")
	}
	return time.Parse(timeLayout, s)
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(strings.ToUpper(err.Error()), "UNIQUE")
}
