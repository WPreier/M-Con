// Package provider defines delivery backends, errors, and the circuit breaker.
package provider

import (
	"context"
	"fmt"
	"sync"
	"time"

	"mcon/internal/message"
)

// Kind classifies a delivery failure.
type Kind int

const (
	// KindTransient can be retried.
	KindTransient Kind = iota + 1
	// KindPermanent must not be retried.
	KindPermanent
	// KindOpen means the circuit breaker rejected the call.
	KindOpen
)

// SendError is a classified provider failure.
type SendError struct {
	Kind     Kind
	Provider string
	Status   int
	Message  string
}

func (e *SendError) Error() string {
	if e == nil {
		return ""
	}
	if e.Status > 0 {
		return fmt.Sprintf("%s: %s (%d)", e.Provider, e.Message, e.Status)
	}
	return fmt.Sprintf("%s: %s", e.Provider, e.Message)
}

// Transient builds a retryable error.
func Transient(name, msg string, status int) error {
	return &SendError{Kind: KindTransient, Provider: name, Status: status, Message: msg}
}

// Permanent builds an error that should dead-letter the message.
func Permanent(name, msg string, status int) error {
	return &SendError{Kind: KindPermanent, Provider: name, Status: status, Message: msg}
}

// Open builds a circuit-open error.
func Open(name string) error {
	return &SendError{Kind: KindOpen, Provider: name, Message: "circuit open"}
}

// IsTransient reports whether err should be retried.
func IsTransient(err error) bool { return kindOf(err) == KindTransient }

// IsPermanent reports whether err must not be retried.
func IsPermanent(err error) bool { return kindOf(err) == KindPermanent }

// IsOpen reports whether the breaker rejected the call.
func IsOpen(err error) bool { return kindOf(err) == KindOpen }

func kindOf(err error) Kind {
	var se *SendError
	if err == nil || !as(err, &se) {
		return 0
	}
	return se.Kind
}

func as(err error, target **SendError) bool {
	for err != nil {
		if se, ok := err.(*SendError); ok {
			*target = se
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// Result is a successful provider response.
type Result struct {
	ProviderMessageID string
	Provider          string
}

// Sender delivers one message.
type Sender interface {
	Send(ctx context.Context, msg message.Outbound) (Result, error)
}

// Breaker tracks consecutive transient failures for one provider.
type Breaker struct {
	mu        sync.Mutex
	state     int
	failures  int
	opened    time.Time
	threshold int
	cooldown  time.Duration
}

const (
	stateClosed = iota
	stateOpen
	stateHalf
)

// NewBreaker returns a closed breaker.
func NewBreaker(threshold int, cooldown time.Duration) *Breaker {
	if threshold < 1 {
		threshold = 5
	}
	if cooldown <= 0 {
		cooldown = 30 * time.Second
	}
	return &Breaker{threshold: threshold, cooldown: cooldown}
}

// Allow returns nil when a call may proceed.
func (b *Breaker) Allow(now time.Time) error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case stateOpen:
		if now.Sub(b.opened) >= b.cooldown {
			b.state = stateHalf
			return nil
		}
		return Open("breaker")
	case stateHalf:
		return Open("breaker")
	default:
		return nil
	}
}

// Success closes the breaker.
func (b *Breaker) Success() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state = stateClosed
	b.failures = 0
}

// Failure records a transient failure and may open the breaker.
func (b *Breaker) Failure(now time.Time) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == stateHalf {
		b.state = stateOpen
		b.opened = now
		return
	}
	b.failures++
	if b.failures >= b.threshold {
		b.state = stateOpen
		b.opened = now
	}
}

// State is closed, open, or half_open.
func (b *Breaker) State() string {
	if b == nil {
		return "closed"
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case stateOpen:
		return "open"
	case stateHalf:
		return "half_open"
	default:
		return "closed"
	}
}

// Registry routes sends through breakers and a single fallback hop.
type Registry struct {
	mu        sync.Mutex
	senders   map[string]Sender
	breakers  map[string]*Breaker
	fallback  map[string]string
	threshold int
	cooldown  time.Duration
	now       func() time.Time
}

// NewRegistry builds an empty registry.
func NewRegistry(threshold int, cooldown time.Duration) *Registry {
	return &Registry{
		senders:   map[string]Sender{},
		breakers:  map[string]*Breaker{},
		fallback:  map[string]string{},
		threshold: threshold,
		cooldown:  cooldown,
		now:       time.Now,
	}
}

// Set registers a sender. An existing breaker is kept.
func (r *Registry) Set(name string, sender Sender, fallback string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.senders[name] = sender
	r.fallback[name] = fallback
	if _, ok := r.breakers[name]; !ok {
		r.breakers[name] = NewBreaker(r.threshold, r.cooldown)
	}
}

// Send delivers msg through name, falling back once on transient or open errors.
func (r *Registry) Send(ctx context.Context, name string, msg message.Outbound) (Result, error) {
	res, err := r.try(ctx, name, msg)
	if err == nil {
		res.Provider = name
		return res, nil
	}
	if IsPermanent(err) {
		return res, err
	}
	fb := r.fallbackOf(name)
	if fb == "" || fb == name {
		return res, err
	}
	res2, err2 := r.try(ctx, fb, msg)
	if err2 == nil {
		res2.Provider = fb
		return res2, nil
	}
	return res2, err2
}

func (r *Registry) fallbackOf(name string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.fallback[name]
}

func (r *Registry) try(ctx context.Context, name string, msg message.Outbound) (Result, error) {
	r.mu.Lock()
	sender := r.senders[name]
	br := r.breakers[name]
	r.mu.Unlock()
	if sender == nil {
		return Result{}, Permanent(name, "unknown provider", 0)
	}
	now := time.Now()
	if r.now != nil {
		now = r.now()
	}
	if err := br.Allow(now); err != nil {
		if se, ok := err.(*SendError); ok {
			se.Provider = name
		}
		return Result{}, err
	}
	res, err := sender.Send(ctx, msg)
	if err == nil {
		br.Success()
		res.Provider = name
		return res, nil
	}
	if !IsPermanent(err) {
		br.Failure(time.Now())
	}
	return res, err
}

// CopyBreakersFrom keeps open circuits across a reload.
func (r *Registry) CopyBreakersFrom(old *Registry) {
	if r == nil || old == nil {
		return
	}
	old.mu.Lock()
	defer old.mu.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	for name, br := range old.breakers {
		if _, ok := r.senders[name]; ok {
			r.breakers[name] = br
		}
	}
}

// BreakerState exposes a breaker for tests and metrics.
func (r *Registry) BreakerState(name string) string {
	r.mu.Lock()
	br := r.breakers[name]
	r.mu.Unlock()
	if br == nil {
		return "closed"
	}
	return br.State()
}
