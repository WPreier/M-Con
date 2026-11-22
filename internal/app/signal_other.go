//go:build !unix

package app

import "context"

func (a *App) watchReload(ctx context.Context) {
	<-ctx.Done()
}
