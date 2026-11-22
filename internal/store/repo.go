package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Recipient is one envelope address.
type Recipient struct {
	Kind    string
	Address string
}

// Attachment is a stored MIME part. Data is empty when blobs were not loaded.
type Attachment struct {
	Filename    string
	ContentType string
	SHA256      string
	Size        int
	Data        []byte
}

// Message is one accepted mail and its queue state.
type Message struct {
	ID                string
	ClientKey         string
	State             string
	Provider          string
	From              string
	ReplyTo           string
	Subject           string
	Text              string
	HTML              string
	Headers           map[string]string
	Tags              []string
	Metadata          map[string]any
	SpamHint          string
	ContentSHA256     string
	BodySHA256        string
	RFC822Path        string
	RFCMessageID      string
	Attempts          int
	MaxAttempts       int
	LeaseUntil        *time.Time
	LeasedBy          string
	NextAttemptAt     *time.Time
	LastError         string
	ProviderMessageID string
	SpamScore         *float64
	SpamReason        string
	SpamVerdict       string
	SpamEngine        string
	ResendOf          string
	SkipSpam          bool
	IdempotencyKey    string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	SentAt            *time.Time
	Recipients        []Recipient
	Attachments       []Attachment
}

// Idempotency remembers a public API response.
type Idempotency struct {
	ClientKey    string
	Key          string
	RequestHash  string
	MessageID    string
	ResponseJSON string
	CreatedAt    time.Time
	ExpiresAt    time.Time
}

// Webhook is one outbox row.
type Webhook struct {
	ID            int64
	MessageID     string
	ClientKey     string
	Event         string
	Payload       string
	Attempts      int
	MaxAttempts   int
	NextAttemptAt *time.Time
	State         string
	LastError     string
	CreatedAt     time.Time
}

// Suppression blocks a recipient address.
type Suppression struct {
	Address   string
	Reason    string
	CreatedAt time.Time
}

const msgCols = `id, client_key, state, provider, from_addr, reply_to, subject, text_body, html_body,
headers_json, tags_json, metadata_json, spam_hint, content_sha256, body_sha256, rfc822_path, rfc_message_id,
attempts, max_attempts, lease_until, leased_by, next_attempt_at, last_error, provider_message_id,
spam_score, spam_reason, spam_verdict, spam_engine, resend_of, skip_spam, idempotency_key,
created_at, updated_at, sent_at`

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// InsertNew stores a message, its parts, and an optional idempotency row in one transaction.
func (s *Store) InsertNew(ctx context.Context, msg *Message, idem *Idempotency) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if idem != nil && idem.Key != "" {
		if _, err := tx.ExecContext(ctx, `DELETE FROM idempotency WHERE client_key=? AND idem_key=? AND expires_at<=?`,
			idem.ClientKey, idem.Key, FormatTime(time.Now().UTC())); err != nil {
			return err
		}
	}
	if err := insertMessage(ctx, tx, msg); err != nil {
		if isUnique(err) {
			return ErrConflict
		}
		return err
	}
	if idem != nil && idem.Key != "" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO idempotency(client_key, idem_key, request_hash, message_id, response_json, created_at, expires_at)
			VALUES(?,?,?,?,?,?,?)`,
			idem.ClientKey, idem.Key, idem.RequestHash, idem.MessageID, idem.ResponseJSON,
			FormatTime(idem.CreatedAt), FormatTime(idem.ExpiresAt)); err != nil {
			if isUnique(err) {
				return ErrConflict
			}
			return err
		}
	}
	return tx.Commit()
}

func insertMessage(ctx context.Context, tx *sql.Tx, msg *Message) error {
	headers, err := json.Marshal(nonNilMap(msg.Headers))
	if err != nil {
		return err
	}
	tags, err := json.Marshal(nonNilSlice(msg.Tags))
	if err != nil {
		return err
	}
	meta := msg.Metadata
	if meta == nil {
		meta = map[string]any{}
	}
	metadata, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	var score any
	if msg.SpamScore != nil {
		score = *msg.SpamScore
	}
	skip := 0
	if msg.SkipSpam {
		skip = 1
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO messages(`+msgCols+`) VALUES(`+placeholders(34)+`)`,
		msg.ID, msg.ClientKey, msg.State, msg.Provider, msg.From, msg.ReplyTo, msg.Subject, msg.Text, msg.HTML,
		string(headers), string(tags), string(metadata), msg.SpamHint, msg.ContentSHA256, msg.BodySHA256, msg.RFC822Path, msg.RFCMessageID,
		msg.Attempts, msg.MaxAttempts, timePtr(msg.LeaseUntil), msg.LeasedBy, timePtr(msg.NextAttemptAt), msg.LastError, msg.ProviderMessageID,
		score, msg.SpamReason, msg.SpamVerdict, msg.SpamEngine, msg.ResendOf, skip, msg.IdempotencyKey,
		FormatTime(msg.CreatedAt), FormatTime(msg.UpdatedAt), timePtr(msg.SentAt),
	)
	if err != nil {
		return err
	}
	for i, rcpt := range msg.Recipients {
		if _, err := tx.ExecContext(ctx, `INSERT INTO recipients(message_id, kind, address, position) VALUES(?,?,?,?)`,
			msg.ID, rcpt.Kind, rcpt.Address, i); err != nil {
			return err
		}
	}
	for i, att := range msg.Attachments {
		if _, err := tx.ExecContext(ctx, `INSERT INTO attachments(message_id, position, filename, content_type, content_sha256, size_bytes, data) VALUES(?,?,?,?,?,?,?)`,
			msg.ID, i, att.Filename, att.ContentType, att.SHA256, att.Size, att.Data); err != nil {
			return err
		}
	}
	return nil
}

func placeholders(n int) string {
	b := make([]byte, 0, n*2)
	for i := 0; i < n; i++ {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, '?')
	}
	return string(b)
}

func nonNilMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func nonNilSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func timePtr(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return FormatTime(t.UTC())
}

// Get loads a message including attachment bytes.
func (s *Store) Get(ctx context.Context, id string) (*Message, error) {
	return s.load(ctx, s.db, id, true)
}

// GetMeta loads a message without attachment bytes.
func (s *Store) GetMeta(ctx context.Context, id string) (*Message, error) {
	return s.load(ctx, s.db, id, false)
}

func (s *Store) load(ctx context.Context, q queryer, id string, blobs bool) (*Message, error) {
	row := q.QueryRowContext(ctx, `SELECT `+msgCols+` FROM messages WHERE id=?`, id)
	msg, err := scanMessage(row)
	if err != nil {
		return nil, err
	}
	rcpt, err := q.QueryContext(ctx, `SELECT kind, address FROM recipients WHERE message_id=? ORDER BY kind, position`, id)
	if err != nil {
		return nil, err
	}
	defer rcpt.Close()
	for rcpt.Next() {
		var r Recipient
		if err := rcpt.Scan(&r.Kind, &r.Address); err != nil {
			return nil, err
		}
		msg.Recipients = append(msg.Recipients, r)
	}
	if err := rcpt.Err(); err != nil {
		return nil, err
	}
	qAtt := `SELECT filename, content_type, content_sha256, size_bytes FROM attachments WHERE message_id=? ORDER BY position`
	if blobs {
		qAtt = `SELECT filename, content_type, content_sha256, size_bytes, data FROM attachments WHERE message_id=? ORDER BY position`
	}
	atts, err := q.QueryContext(ctx, qAtt, id)
	if err != nil {
		return nil, err
	}
	defer atts.Close()
	for atts.Next() {
		var a Attachment
		if blobs {
			if err := atts.Scan(&a.Filename, &a.ContentType, &a.SHA256, &a.Size, &a.Data); err != nil {
				return nil, err
			}
		} else if err := atts.Scan(&a.Filename, &a.ContentType, &a.SHA256, &a.Size); err != nil {
			return nil, err
		}
		msg.Attachments = append(msg.Attachments, a)
	}
	return msg, atts.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanMessage(row scanner) (*Message, error) {
	var msg Message
	var headers, tags, metadata string
	var lease, next, sent sql.NullString
	var score sql.NullFloat64
	var skip int
	var created, updated string
	err := row.Scan(
		&msg.ID, &msg.ClientKey, &msg.State, &msg.Provider, &msg.From, &msg.ReplyTo, &msg.Subject, &msg.Text, &msg.HTML,
		&headers, &tags, &metadata, &msg.SpamHint, &msg.ContentSHA256, &msg.BodySHA256, &msg.RFC822Path, &msg.RFCMessageID,
		&msg.Attempts, &msg.MaxAttempts, &lease, &msg.LeasedBy, &next, &msg.LastError, &msg.ProviderMessageID,
		&score, &msg.SpamReason, &msg.SpamVerdict, &msg.SpamEngine, &msg.ResendOf, &skip, &msg.IdempotencyKey,
		&created, &updated, &sent,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(headers), &msg.Headers); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(tags), &msg.Tags); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(metadata), &msg.Metadata); err != nil {
		return nil, err
	}
	msg.SkipSpam = skip == 1
	if score.Valid {
		v := score.Float64
		msg.SpamScore = &v
	}
	msg.CreatedAt, err = ParseTime(created)
	if err != nil {
		return nil, err
	}
	msg.UpdatedAt, err = ParseTime(updated)
	if err != nil {
		return nil, err
	}
	if t, err := nullTime(lease); err != nil {
		return nil, err
	} else {
		msg.LeaseUntil = t
	}
	if t, err := nullTime(next); err != nil {
		return nil, err
	} else {
		msg.NextAttemptAt = t
	}
	if t, err := nullTime(sent); err != nil {
		return nil, err
	} else {
		msg.SentAt = t
	}
	return &msg, nil
}

func nullTime(v sql.NullString) (*time.Time, error) {
	if !v.Valid || v.String == "" {
		return nil, nil
	}
	t, err := ParseTime(v.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// List returns recent messages without attachment bytes.
func (s *Store) List(ctx context.Context, client, state string, limit int) ([]*Message, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT ` + msgCols + ` FROM messages WHERE 1=1`
	var args []any
	if client != "" {
		q += ` AND client_key=?`
		args = append(args, client)
	}
	if state != "" {
		q += ` AND state=?`
		args = append(args, state)
	}
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	var out []*Message
	for rows.Next() {
		msg, err := scanMessage(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, msg)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for _, msg := range out {
		rcpt, err := s.db.QueryContext(ctx, `SELECT kind, address FROM recipients WHERE message_id=? ORDER BY kind, position`, msg.ID)
		if err != nil {
			return nil, err
		}
		for rcpt.Next() {
			var r Recipient
			if err := rcpt.Scan(&r.Kind, &r.Address); err != nil {
				rcpt.Close()
				return nil, err
			}
			msg.Recipients = append(msg.Recipients, r)
		}
		if err := rcpt.Err(); err != nil {
			rcpt.Close()
			return nil, err
		}
		rcpt.Close()
	}
	return out, nil
}

// GetIdempotency returns a stored response, including expired rows.
func (s *Store) GetIdempotency(ctx context.Context, client, key string) (*Idempotency, error) {
	var rec Idempotency
	var created, expires string
	err := s.db.QueryRowContext(ctx, `SELECT client_key, idem_key, request_hash, message_id, response_json, created_at, expires_at
		FROM idempotency WHERE client_key=? AND idem_key=?`, client, key).Scan(
		&rec.ClientKey, &rec.Key, &rec.RequestHash, &rec.MessageID, &rec.ResponseJSON, &created, &expires)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rec.CreatedAt, err = ParseTime(created)
	if err != nil {
		return nil, err
	}
	rec.ExpiresAt, err = ParseTime(expires)
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// SaveIdempotency inserts a key that points at an existing message.
func (s *Store) SaveIdempotency(ctx context.Context, rec Idempotency) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO idempotency(client_key, idem_key, request_hash, message_id, response_json, created_at, expires_at)
		VALUES(?,?,?,?,?,?,?)`,
		rec.ClientKey, rec.Key, rec.RequestHash, rec.MessageID, rec.ResponseJSON, FormatTime(rec.CreatedAt), FormatTime(rec.ExpiresAt))
	if isUnique(err) {
		return ErrConflict
	}
	return err
}

// FindDedupe returns the newest original message with the same body hash inside the window.
func (s *Store) FindDedupe(ctx context.Context, client, bodyHash string, since time.Time) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM messages
		WHERE client_key=? AND body_sha256=? AND created_at>=? AND resend_of='' AND state!='cancelled'
		ORDER BY created_at DESC LIMIT 1`, client, bodyHash, FormatTime(since.UTC())).Scan(&id)
	if err == sql.ErrNoRows {
		return "", ErrNotFound
	}
	return id, err
}

// Claim leases due messages and moves them to spam_check or sending.
func (s *Store) Claim(ctx context.Context, worker string, lease time.Duration, limit int, now time.Time) ([]*Message, error) {
	if limit <= 0 {
		limit = 1
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	nowS := FormatTime(now.UTC())
	until := FormatTime(now.UTC().Add(lease))
	rows, err := tx.QueryContext(ctx, `SELECT id FROM messages
		WHERE state IN ('received','spam_check','queued','retry_wait','sending')
		  AND (lease_until IS NULL OR lease_until <= ?)
		  AND (next_attempt_at IS NULL OR next_attempt_at <= ?)
		ORDER BY created_at LIMIT ?`, nowS, nowS, limit)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE messages SET
			state = CASE
				WHEN state IN ('queued','retry_wait','sending') THEN 'sending'
				WHEN skip_spam = 1 AND state IN ('received','spam_check') THEN 'sending'
				WHEN state = 'received' THEN 'spam_check'
				ELSE state
			END,
			lease_until=?, leased_by=?, updated_at=?
			WHERE id=?`, until, worker, nowS, id); err != nil {
			return nil, err
		}
	}
	var out []*Message
	for _, id := range ids {
		msg, err := s.load(ctx, tx, id, true)
		if err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// SaveSpam records a classifier result without releasing the lease.
func (s *Store) SaveSpam(ctx context.Context, id string, score float64, reason, verdict, engine string, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE messages SET spam_score=?, spam_reason=?, spam_verdict=?, spam_engine=?, updated_at=? WHERE id=?`,
		score, reason, verdict, engine, FormatTime(now.UTC()), id)
	return rowsAffected(res, err)
}

// MarkBlocked finishes a message as blocked and releases the lease.
func (s *Store) MarkBlocked(ctx context.Context, id, reason, engine string, score float64, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE messages SET state='blocked', spam_verdict='block', spam_reason=?, spam_engine=?, spam_score=?,
		last_error=?, lease_until=NULL, leased_by='', updated_at=? WHERE id=?`,
		reason, engine, score, reason, FormatTime(now.UTC()), id)
	return rowsAffected(res, err)
}

// MarkSent records a successful delivery.
func (s *Store) MarkSent(ctx context.Context, id, provider, providerID string, attempts int, now time.Time) error {
	ts := FormatTime(now.UTC())
	res, err := s.db.ExecContext(ctx, `UPDATE messages SET state='sent', provider=?, provider_message_id=?, attempts=?, sent_at=?,
		last_error='', lease_until=NULL, leased_by='', updated_at=? WHERE id=?`,
		provider, providerID, attempts, ts, ts, id)
	return rowsAffected(res, err)
}

// MarkRetry schedules another attempt.
func (s *Store) MarkRetry(ctx context.Context, id string, attempts int, next time.Time, lastErr string, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE messages SET state='retry_wait', attempts=?, next_attempt_at=?, last_error=?,
		lease_until=NULL, leased_by='', updated_at=? WHERE id=?`,
		attempts, FormatTime(next.UTC()), lastErr, FormatTime(now.UTC()), id)
	return rowsAffected(res, err)
}

// MarkDead moves a message to the dead-letter state.
func (s *Store) MarkDead(ctx context.Context, id string, attempts int, lastErr string, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE messages SET state='dead', attempts=?, last_error=?,
		lease_until=NULL, leased_by='', updated_at=? WHERE id=?`,
		attempts, lastErr, FormatTime(now.UTC()), id)
	return rowsAffected(res, err)
}

// Requeue returns a leased message to a waiting state without counting an attempt.
func (s *Store) Requeue(ctx context.Context, id, state string, now time.Time) error {
	if state == "" {
		state = "queued"
	}
	res, err := s.db.ExecContext(ctx, `UPDATE messages SET state=?, lease_until=NULL, leased_by='', updated_at=? WHERE id=?`,
		state, FormatTime(now.UTC()), id)
	return rowsAffected(res, err)
}

// Cancel aborts delivery when the message is not currently leased.
func (s *Store) Cancel(ctx context.Context, client, id string, now time.Time) error {
	nowS := FormatTime(now.UTC())
	res, err := s.db.ExecContext(ctx, `UPDATE messages SET state='cancelled', lease_until=NULL, leased_by='', updated_at=?
		WHERE id=? AND client_key=? AND state IN ('received','spam_check','queued','retry_wait')
		  AND (lease_until IS NULL OR lease_until <= ?)`, nowS, id, client, nowS)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 1 {
		return nil
	}
	msg, err := s.GetMeta(ctx, id)
	if err != nil {
		return err
	}
	if msg.ClientKey != client {
		return ErrNotFound
	}
	return ErrState
}

// Replay queues a dead message again.
func (s *Store) Replay(ctx context.Context, id string, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE messages SET state='queued', attempts=0, last_error='', next_attempt_at=NULL,
		lease_until=NULL, leased_by='', updated_at=? WHERE id=? AND state='dead'`, FormatTime(now.UTC()), id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 1 {
		return nil
	}
	if _, err := s.GetMeta(ctx, id); err != nil {
		return err
	}
	return ErrState
}

// CountByState returns message counts keyed by state.
func (s *Store) CountByState(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT state, COUNT(*) FROM messages GROUP BY state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var state string
		var n int
		if err := rows.Scan(&state, &n); err != nil {
			return nil, err
		}
		out[state] = n
	}
	return out, rows.Err()
}

func rowsAffected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// UseNonce records a nonce or returns ErrReplay.
func (s *Store) UseNonce(ctx context.Context, client, nonce string, expires time.Time) error {
	now := FormatTime(time.Now().UTC())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM nonces WHERE expires_at <= ?`, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO nonces(client_key, nonce, expires_at) VALUES(?,?,?)`,
		client, nonce, FormatTime(expires.UTC())); err != nil {
		if isUnique(err) {
			return ErrReplay
		}
		return err
	}
	return tx.Commit()
}

// EnqueueWebhook adds an outbox row.
func (s *Store) EnqueueWebhook(ctx context.Context, client, messageID, event, payload string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO webhooks(message_id, client_key, event, payload, attempts, max_attempts, state, created_at)
		VALUES(?,?,?,?,0,8,'pending',?)`, messageID, client, event, payload, FormatTime(now.UTC()))
	return err
}

// ClaimWebhooks leases pending outbox rows by incrementing the attempt counter.
func (s *Store) ClaimWebhooks(ctx context.Context, limit int, now time.Time) ([]Webhook, error) {
	if limit <= 0 {
		limit = 10
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id FROM webhooks
		WHERE state='pending' AND (next_attempt_at IS NULL OR next_attempt_at <= ?)
		ORDER BY id LIMIT ?`, FormatTime(now.UTC()), limit)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	var out []Webhook
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE webhooks SET attempts=attempts+1, next_attempt_at=NULL WHERE id=?`, id); err != nil {
			return nil, err
		}
		var h Webhook
		var next sql.NullString
		var created string
		err := tx.QueryRowContext(ctx, `SELECT id, message_id, client_key, event, payload, attempts, max_attempts, next_attempt_at, state, last_error, created_at
			FROM webhooks WHERE id=?`, id).Scan(&h.ID, &h.MessageID, &h.ClientKey, &h.Event, &h.Payload, &h.Attempts, &h.MaxAttempts, &next, &h.State, &h.LastError, &created)
		if err != nil {
			return nil, err
		}
		h.CreatedAt, err = ParseTime(created)
		if err != nil {
			return nil, err
		}
		if t, err := nullTime(next); err != nil {
			return nil, err
		} else {
			h.NextAttemptAt = t
		}
		out = append(out, h)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// FinishWebhook records the delivery result.
func (s *Store) FinishWebhook(ctx context.Context, id int64, state, lastErr string, next *time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE webhooks SET state=?, last_error=?, next_attempt_at=? WHERE id=?`,
		state, lastErr, timePtr(next), id)
	return rowsAffected(res, err)
}

// Suppress adds or replaces a blocked address.
func (s *Store) Suppress(ctx context.Context, address, reason string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO suppressions(address, reason, created_at) VALUES(?,?,?)
		ON CONFLICT(address) DO UPDATE SET reason=excluded.reason, created_at=excluded.created_at`,
		address, reason, FormatTime(now.UTC()))
	return err
}

// Unsuppress removes an address.
func (s *Store) Unsuppress(ctx context.Context, address string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM suppressions WHERE address=?`, address)
	return rowsAffected(res, err)
}

// ListSuppressions returns the block list.
func (s *Store) ListSuppressions(ctx context.Context) ([]Suppression, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT address, reason, created_at FROM suppressions ORDER BY address`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Suppression
	for rows.Next() {
		var item Suppression
		var created string
		if err := rows.Scan(&item.Address, &item.Reason, &created); err != nil {
			return nil, err
		}
		item.CreatedAt, err = ParseTime(created)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// Suppressed returns which of the addresses are blocked.
func (s *Store) Suppressed(ctx context.Context, addresses []string) ([]string, error) {
	var hit []string
	for _, addr := range addresses {
		var n int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM suppressions WHERE address=?`, addr).Scan(&n); err != nil {
			return nil, err
		}
		if n > 0 {
			hit = append(hit, addr)
		}
	}
	return hit, nil
}

// ForeignKeys reports whether foreign_keys is on.
func (s *Store) ForeignKeys(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&n)
	return n, err
}

// AppliedMigrations lists applied versions.
func (s *Store) AppliedMigrations(ctx context.Context) ([]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// DebugExec runs SQL from tests in this module.
func (s *Store) DebugExec(ctx context.Context, q string, args ...any) error {
	_, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("debug exec: %w", err)
	}
	return nil
}
