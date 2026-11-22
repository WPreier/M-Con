// Package discard accepts or fails messages without delivering them.
package discard

import (
	"context"
	"sync"

	"mcon/internal/message"
	"mcon/internal/provider"
)

// Provider is a test and drain backend.
type Provider struct {
	Mode      string
	FailTimes int
	mu        sync.Mutex
	calls     int
}

// Send counts the call and returns success or a forced error.
func (p *Provider) Send(ctx context.Context, msg message.Outbound) (provider.Result, error) {
	if err := ctx.Err(); err != nil {
		return provider.Result{}, err
	}
	p.mu.Lock()
	p.calls++
	n := p.calls
	mode := p.Mode
	failTimes := p.FailTimes
	p.mu.Unlock()
	if mode == "" {
		mode = "success"
	}
	if failTimes > 0 {
		if n <= failTimes {
			if mode == "permanent" {
				return provider.Result{}, provider.Permanent("discard", "forced permanent", 550)
			}
			return provider.Result{}, provider.Transient("discard", "forced transient", 450)
		}
		return provider.Result{ProviderMessageID: "discard-" + msg.ID}, nil
	}
	switch mode {
	case "permanent":
		return provider.Result{}, provider.Permanent("discard", "forced permanent", 550)
	case "transient":
		return provider.Result{}, provider.Transient("discard", "forced transient", 450)
	default:
		return provider.Result{ProviderMessageID: "discard-" + msg.ID}, nil
	}
}

// Calls returns how many deliveries were attempted.
func (p *Provider) Calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}
