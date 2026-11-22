package provider

import (
	"context"
	"testing"
	"time"

	"mcon/internal/message"
)

type fakeSender struct {
	fails int
	calls int
	name  string
}

func (f *fakeSender) Send(ctx context.Context, msg message.Outbound) (Result, error) {
	f.calls++
	if f.calls <= f.fails {
		return Result{}, Transient(f.name, "down", 503)
	}
	return Result{ProviderMessageID: "ok"}, nil
}

func TestBreakerOpensAndFallback(t *testing.T) {
	reg := NewRegistry(2, 30*time.Millisecond)
	primary := &fakeSender{fails: 10, name: "primary"}
	backup := &fakeSender{name: "backup"}
	reg.Set("primary", primary, "backup")
	reg.Set("backup", backup, "")
	msg := message.Outbound{ID: "m1", From: "a@b.c"}
	for i := 0; i < 2; i++ {
		res, err := reg.Send(context.Background(), "primary", msg)
		if err != nil || res.Provider != "backup" {
			t.Fatalf("fallback i=%d res=%+v err=%v", i, res, err)
		}
	}
	if reg.BreakerState("primary") != "open" {
		t.Fatalf("state %s", reg.BreakerState("primary"))
	}
	if primary.calls != 2 {
		t.Fatalf("primary calls %d", primary.calls)
	}
	res, err := reg.Send(context.Background(), "primary", msg)
	if err != nil || res.Provider != "backup" {
		t.Fatalf("open fallback %+v %v", res, err)
	}
	if primary.calls != 2 {
		t.Fatalf("breaker did not short-circuit: %d", primary.calls)
	}
	time.Sleep(40 * time.Millisecond)
	primary.fails = 0
	res, err = reg.Send(context.Background(), "primary", msg)
	if err != nil || res.Provider != "primary" {
		t.Fatalf("half-open %+v %v", res, err)
	}
}

func TestPermanentDoesNotFallback(t *testing.T) {
	reg := NewRegistry(5, time.Second)
	reg.Set("primary", senderFunc(func(ctx context.Context, msg message.Outbound) (Result, error) {
		return Result{}, Permanent("primary", "bad recipient", 550)
	}), "backup")
	called := false
	reg.Set("backup", senderFunc(func(ctx context.Context, msg message.Outbound) (Result, error) {
		called = true
		return Result{ProviderMessageID: "no"}, nil
	}), "")
	_, err := reg.Send(context.Background(), "primary", message.Outbound{ID: "1"})
	if !IsPermanent(err) || called {
		t.Fatalf("err %v called %v", err, called)
	}
}

type senderFunc func(ctx context.Context, msg message.Outbound) (Result, error)

func (f senderFunc) Send(ctx context.Context, msg message.Outbound) (Result, error) {
	return f(ctx, msg)
}
