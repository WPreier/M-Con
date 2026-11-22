//go:build unix

package app

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func (a *App) watchReload(ctx context.Context) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP)
	defer signal.Stop(ch)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ch:
			if err := a.Reload(); err != nil {
				a.log.Warn("reload rejected", "error", err.Error())
			}
		}
	}
}
