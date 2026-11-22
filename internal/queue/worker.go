package queue

import (
	"context"
	"time"

	"mcon/internal/archive"
	"mcon/internal/config"
	"mcon/internal/logging"
	"mcon/internal/message"
	"mcon/internal/metrics"
	"mcon/internal/provider"
	"mcon/internal/spam"
	"mcon/internal/store"
	"mcon/internal/webhook"
)

// Manager polls the queue and delivers claimed messages.
type Manager struct {
	Store   *store.Store
	Reg     func() *provider.Registry
	Spam    *spam.Pipeline
	Hooks   *webhook.Dispatcher
	Archive archive.Archive
	Metrics *metrics.Metrics
	Log     *logging.Logger
	Cfg     func() config.Snapshot
	ID      string
}

// Run polls until ctx is cancelled.
func (m *Manager) Run(ctx context.Context) {
	interval := 500 * time.Millisecond
	if m.Cfg != nil {
		if d := m.Cfg().Config.Queue.PollInterval.Std(); d > 0 {
			interval = d
		}
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = m.Tick(ctx, time.Now())
		}
	}
}

// Tick claims a batch and delivers it. now is the claim clock.
func (m *Manager) Tick(ctx context.Context, now time.Time) (int, error) {
	lease := time.Minute
	limit := 8
	if m.Cfg != nil {
		q := m.Cfg().Config.Queue
		if q.Lease.Std() > 0 {
			lease = q.Lease.Std()
		}
		if q.Workers > 0 {
			limit = q.Workers
		}
	}
	msgs, err := m.Store.Claim(ctx, m.ID, lease, limit, now)
	if err != nil {
		return 0, err
	}
	for _, msg := range msgs {
		m.process(ctx, msg, now)
	}
	if m.Hooks != nil {
		_ = m.Hooks.Drain(ctx, now, 20)
	}
	return len(msgs), nil
}

func (m *Manager) process(ctx context.Context, msg *store.Message, now time.Time) {
	if ctx.Err() != nil {
		_ = m.Store.Requeue(context.Background(), msg.ID, waitState(msg), time.Now())
		return
	}
	if msg.State == "spam_check" && msg.SpamVerdict == "" && !msg.SkipSpam && m.Spam != nil {
		dec, err := m.Spam.Evaluate(ctx, spam.Input{
			From: msg.From, To: recipients(msg, "to"), Subject: msg.Subject,
			Text: msg.Text, HTML: msg.HTML, Hint: msg.SpamHint,
		})
		if ctx.Err() != nil {
			_ = m.Store.Requeue(context.Background(), msg.ID, "spam_check", time.Now())
			return
		}
		if err != nil {
			dec = spam.Decision{Action: "send", Engine: "error", Reason: err.Error()}
		}
		if m.Metrics != nil {
			m.Metrics.Inc("mcon_spam_decisions_total", map[string]string{"action": dec.Action, "engine": dec.Engine})
		}
		if dec.Action == "block" {
			_ = m.Store.MarkBlocked(ctx, msg.ID, dec.Reason, dec.Engine, dec.Score, now)
			m.emit(ctx, msg, "blocked", "blocked", now)
			return
		}
		_ = m.Store.SaveSpam(ctx, msg.ID, dec.Score, dec.Reason, "send", dec.Engine, now)
	}
	out := toOutbound(msg)
	if msg.RFC822Path != "" {
		raw, err := m.Archive.Read(msg.RFC822Path)
		if err != nil {
			m.fail(ctx, msg, now, provider.Transient("archive", err.Error(), 0))
			return
		}
		out.Raw = raw
	} else if raw, err := message.Build(out); err == nil {
		out.Raw = raw
	}
	reg := m.Reg()
	if reg == nil {
		m.fail(ctx, msg, now, provider.Permanent(msg.Provider, "no provider registry", 0))
		return
	}
	res, err := reg.Send(ctx, msg.Provider, out)
	if ctx.Err() != nil {
		state := "queued"
		if msg.SpamVerdict == "" && !msg.SkipSpam {
			state = "spam_check"
		}
		_ = m.Store.Requeue(context.Background(), msg.ID, state, time.Now())
		return
	}
	if err == nil {
		name := msg.Provider
		if res.Provider != "" {
			name = res.Provider
		}
		attempts := msg.Attempts + 1
		_ = m.Store.MarkSent(ctx, msg.ID, name, res.ProviderMessageID, attempts, now)
		msg.Provider = name
		m.emit(ctx, msg, "sent", "sent", now)
		if m.Metrics != nil {
			m.Metrics.Inc("mcon_provider_results_total", map[string]string{"provider": name, "result": "sent"})
		}
		return
	}
	m.fail(ctx, msg, now, err)
}

func (m *Manager) fail(ctx context.Context, msg *store.Message, now time.Time, err error) {
	attempts := msg.Attempts + 1
	result := "transient"
	if provider.IsPermanent(err) {
		result = "permanent"
	}
	if provider.IsOpen(err) {
		result = "open"
	}
	if m.Metrics != nil {
		m.Metrics.Inc("mcon_provider_results_total", map[string]string{"provider": msg.Provider, "result": result})
	}
	text := err.Error()
	if len(text) > 500 {
		text = text[:500]
	}
	if m.Log != nil {
		m.Log.Warn("delivery failed", "message_id", msg.ID, "provider", msg.Provider, "error", text)
	}
	if provider.IsPermanent(err) || attempts >= msg.MaxAttempts {
		_ = m.Store.MarkDead(ctx, msg.ID, attempts, text, now)
		m.emit(ctx, msg, "dead", "dead", now)
		return
	}
	initial, max := 5*time.Second, time.Hour
	mult := 2.0
	if m.Cfg != nil {
		q := m.Cfg().Config.Queue
		if q.BackoffInitial.Std() > 0 {
			initial = q.BackoffInitial.Std()
		}
		if q.BackoffMax.Std() > 0 {
			max = q.BackoffMax.Std()
		}
		if q.BackoffMultiplier >= 1 {
			mult = q.BackoffMultiplier
		}
	}
	next := now.Add(Backoff(attempts, initial, max, mult))
	_ = m.Store.MarkRetry(ctx, msg.ID, attempts, next, text, now)
}

func (m *Manager) emit(ctx context.Context, msg *store.Message, event, state string, now time.Time) {
	if m.Hooks == nil {
		return
	}
	_ = m.Hooks.Emit(ctx, msg.ClientKey, msg.ID, event, state, msg.Provider, now)
}

func toOutbound(msg *store.Message) message.Outbound {
	out := message.Outbound{
		ID: msg.ID, From: msg.From, ReplyTo: msg.ReplyTo, Subject: msg.Subject,
		Text: msg.Text, HTML: msg.HTML, Headers: msg.Headers, MessageID: msg.RFCMessageID,
	}
	for _, rcpt := range msg.Recipients {
		switch rcpt.Kind {
		case "cc":
			out.Cc = append(out.Cc, rcpt.Address)
		case "bcc":
			out.Bcc = append(out.Bcc, rcpt.Address)
		default:
			out.To = append(out.To, rcpt.Address)
		}
	}
	for _, att := range msg.Attachments {
		out.Attachments = append(out.Attachments, message.Attachment{Filename: att.Filename, ContentType: att.ContentType, Data: att.Data})
	}
	return out
}

func recipients(msg *store.Message, kind string) []string {
	var out []string
	for _, rcpt := range msg.Recipients {
		if rcpt.Kind == kind {
			out = append(out, rcpt.Address)
		}
	}
	return out
}

func waitState(msg *store.Message) string {
	if msg.SkipSpam || msg.SpamVerdict != "" {
		return "queued"
	}
	return "spam_check"
}
