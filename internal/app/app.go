// Package app wires the daemon and runs its listeners.
package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"mcon/internal/archive"
	"mcon/internal/auth"
	"mcon/internal/config"
	"mcon/internal/httpapi"
	"mcon/internal/logging"
	"mcon/internal/metrics"
	"mcon/internal/provider"
	"mcon/internal/queue"
	"mcon/internal/sdnotify"
	"mcon/internal/service"
	"mcon/internal/spam"
	"mcon/internal/store"
	"mcon/internal/version"
	"mcon/internal/webhook"
)

// App is a running M-Con service.
type App struct {
	snap        atomic.Pointer[config.Snapshot]
	reg         atomic.Pointer[provider.Registry]
	store       *store.Store
	log         *logging.Logger
	metrics     *metrics.Metrics
	queue       *queue.Manager
	configPath  string
	secretsPath string
	publicHTTP  *http.Server
	adminHTTP   *http.Server
}

type writeCloser struct {
	io.Writer
	close func() error
}

func (w writeCloser) Close() error {
	if w.close == nil {
		return nil
	}
	return w.close()
}

// Open validates configuration, opens SQLite, and wires the workers.
func Open(cfg config.Config, sec config.Secrets, configPath, secretsPath string) (*App, error) {
	if err := config.Validate(cfg, sec); err != nil {
		return nil, err
	}
	if err := config.Prepare(cfg); err != nil {
		return nil, err
	}
	log := logging.New(nil, cfg.Log.Level, nil)
	if cfg.Log.Syslog.Enabled {
		w, err := logging.OpenSyslog(cfg.Log.Syslog.Network, cfg.Log.Syslog.Address, cfg.Log.Syslog.Tag, cfg.Log.Syslog.Facility)
		if err != nil {
			log.Warn("syslog unavailable", "error", err.Error())
		} else {
			log.SetExtra(writeCloser{Writer: w, close: w.Close})
		}
	}
	st, err := store.Open(cfg.Paths.DBFile)
	if err != nil {
		return nil, err
	}
	a := &App{
		store: st, log: log, metrics: metrics.New(),
		configPath: configPath, secretsPath: secretsPath,
	}
	snap := cfg
	secCopy := sec
	a.snap.Store(&config.Snapshot{Config: snap, Secrets: secCopy})
	a.reg.Store(buildRegistry(cfg, sec))
	arch := archive.Archive{Dir: cfg.Paths.ArchiveDir}
	hooks := &webhook.Dispatcher{
		Store: st, Metrics: a.metrics, Log: log,
		Endpoint: func(client string) (string, string, bool) {
			cur := a.snapshot()
			cl, ok := cur.Config.Clients[client]
			if !ok || cl.WebhookURL == "" {
				return "", "", false
			}
			secret := cur.Secrets.Clients[client].WebhookSecret
			return cl.WebhookURL, secret, secret != ""
		},
	}
	svc := &service.Service{Store: st, Archive: arch, Hooks: hooks, Metrics: a.metrics, Cfg: a.snapshot}
	pipe := &spam.Pipeline{
		HTTP:   &http.Client{},
		Cfg:    func() config.Spam { return a.snapshot().Config.Spam },
		APIKey: func() string { return a.snapshot().Secrets.Spam.OpenRouter.APIKey },
	}
	a.queue = &queue.Manager{
		Store: st, Spam: pipe, Hooks: hooks, Archive: arch, Metrics: a.metrics, Log: log, ID: "mcon",
		Cfg: a.snapshot,
		Reg: func() *provider.Registry { return a.reg.Load() },
	}
	api := &httpapi.API{
		Snap: a.snapshot, Store: st, Limit: auth.NewLimiter(), Svc: svc, Metrics: a.metrics, Archive: arch,
		Version: version.Version, Reload: a.Reload,
		Ready: func(r *http.Request) error { return st.Ping(r.Context()) },
		Auth:  &refreshingAuth{app: a, nonces: st},
	}
	a.publicHTTP = &http.Server{Handler: api.Public()}
	a.adminHTTP = &http.Server{Handler: api.Admin()}
	return a, nil
}

type refreshingAuth struct {
	app    *App
	nonces auth.NonceStore
}

func (r *refreshingAuth) VerifyRequest(ctx context.Context, method, path string, header http.Header, body []byte) (string, error) {
	snap := r.app.snapshot()
	inner := &auth.Authenticator{
		Now: time.Now, Window: snap.Config.Security.HMACWindow.Std(), NonceTTL: snap.Config.Security.NonceTTL.Std(),
		Nonces: r.nonces,
		Secret: func(keyID string) (string, bool) {
			sec, ok := snap.Secrets.Clients[keyID]
			if !ok || sec.HMACSecret == "" {
				return "", false
			}
			return sec.HMACSecret, true
		},
	}
	return inner.VerifyRequest(ctx, method, path, header, body)
}

func (a *App) snapshot() config.Snapshot {
	if s := a.snap.Load(); s != nil {
		return *s
	}
	return config.Snapshot{}
}

// Reload reads both files again and swaps them in only when they validate.
func (a *App) Reload() error {
	if a.configPath == "" || a.secretsPath == "" {
		return errors.New("reload paths are not set")
	}
	cfg, err := config.Load(a.configPath)
	if err != nil {
		return err
	}
	sec, err := config.LoadSecrets(a.secretsPath)
	if err != nil {
		return err
	}
	if err := config.Validate(cfg, sec); err != nil {
		return err
	}
	cur := a.snapshot().Config
	if cfg.Server.PublicAddr != cur.Server.PublicAddr || cfg.Server.AdminAddr != cur.Server.AdminAddr {
		return errors.New("listen addresses cannot change without a restart")
	}
	next := buildRegistry(cfg, sec)
	if old := a.reg.Load(); old != nil {
		next.CopyBreakersFrom(old)
	}
	a.reg.Store(next)
	a.snap.Store(&config.Snapshot{Config: cfg, Secrets: sec})
	a.log.Info("configuration reloaded")
	return nil
}

// Run listens on the configured addresses until ctx is cancelled.
func (a *App) Run(ctx context.Context) error {
	snap := a.snapshot().Config
	publicLn, err := net.Listen("tcp", snap.Server.PublicAddr)
	if err != nil {
		return err
	}
	if snap.Server.TLS.Enabled {
		cert, err := tls.LoadX509KeyPair(snap.Server.TLS.CertFile, snap.Server.TLS.KeyFile)
		if err != nil {
			_ = publicLn.Close()
			return err
		}
		publicLn = tls.NewListener(publicLn, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	}
	adminLn, err := net.Listen("tcp", snap.Server.AdminAddr)
	if err != nil {
		_ = publicLn.Close()
		return err
	}
	return a.Serve(ctx, publicLn, adminLn)
}

// Serve runs workers and the two listeners until ctx is cancelled.
func (a *App) Serve(ctx context.Context, publicLn, adminLn net.Listener) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go a.watchReload(ctx)
	go a.queue.Run(ctx)
	go a.backupLoop(ctx)
	go a.watchdog(ctx)
	errCh := make(chan error, 2)
	go func() { errCh <- a.serve(a.publicHTTP, publicLn) }()
	go func() { errCh <- a.serve(a.adminHTTP, adminLn) }()
	sdnotify.Ready()
	a.log.Info("listening", "public", publicLn.Addr().String(), "admin", adminLn.Addr().String())
	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			cancel()
			sdnotify.Stopping()
			return err
		}
	}
	sdnotify.Stopping()
	shutCtx, shutCancel := context.WithTimeout(context.Background(), a.snapshot().Config.Server.ShutdownTimeout.Std())
	defer shutCancel()
	_ = a.publicHTTP.Shutdown(shutCtx)
	_ = a.adminHTTP.Shutdown(shutCtx)
	_ = a.store.Close()
	_ = a.log.Close()
	return nil
}

func (a *App) serve(srv *http.Server, ln net.Listener) error {
	snap := a.snapshot().Config.Server
	srv.ReadTimeout = snap.ReadTimeout.Std()
	srv.WriteTimeout = snap.WriteTimeout.Std()
	srv.IdleTimeout = snap.IdleTimeout.Std()
	err := srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (a *App) backupLoop(ctx context.Context) {
	for {
		interval := a.snapshot().Config.Backup.Interval.Std()
		if !a.snapshot().Config.Backup.Enabled || interval <= 0 {
			return
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			dest := fmt.Sprintf("%s/mcon-%s.db", a.snapshot().Config.Paths.BackupDir, time.Now().UTC().Format("20060102"))
			if err := a.store.Backup(ctx, dest); err != nil {
				a.log.Warn("backup failed", "error", err.Error())
			} else {
				a.log.Info("backup written")
			}
		}
	}
}

func (a *App) watchdog(ctx context.Context) {
	interval := a.snapshot().Config.Server.WatchdogInterval.Std()
	if interval <= 0 {
		interval = 10 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sdnotify.Watchdog()
			_ = a.store.Cleanup(ctx, time.Now())
		}
	}
}

// Close releases the database if Serve was not used.
func (a *App) Close() error {
	if a.store != nil {
		return a.store.Close()
	}
	return nil
}
