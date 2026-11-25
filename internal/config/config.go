// Package config loads and validates mcon.yaml and secrets.yaml.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration is a time.Duration that unmarshals from a Go duration string.
type Duration time.Duration

// Std returns the standard library duration.
func (d Duration) Std() time.Duration { return time.Duration(d) }

// UnmarshalYAML parses a duration string such as 5s or 1h.
func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	var s string
	if err := n.Decode(&s); err != nil {
		return err
	}
	if strings.TrimSpace(s) == "" {
		*d = 0
		return nil
	}
	p, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("duration %q: %w", s, err)
	}
	*d = Duration(p)
	return nil
}

// Config is the non-secret service configuration.
type Config struct {
	Server         Server            `yaml:"server"`
	Paths          Paths             `yaml:"paths"`
	Log            Log               `yaml:"log"`
	Metrics        Metrics           `yaml:"metrics"`
	Queue          Queue             `yaml:"queue"`
	CircuitBreaker Breaker           `yaml:"circuit_breaker"`
	Spam           Spam              `yaml:"spam"`
	Providers      Providers         `yaml:"providers"`
	Clients        map[string]Client `yaml:"clients"`
	Archive        Archive           `yaml:"archive"`
	Idempotency    Idempotency       `yaml:"idempotency"`
	Security       Security          `yaml:"security"`
	Dedupe         Dedupe            `yaml:"dedupe"`
	Backup         Backup            `yaml:"backup"`
	TestUI         TestUI            `yaml:"test_ui"`
}

// Server describes the public and admin listeners.
type Server struct {
	PublicAddr       string   `yaml:"public_addr"`
	AdminAddr        string   `yaml:"admin_addr"`
	ReadTimeout      Duration `yaml:"read_timeout"`
	WriteTimeout     Duration `yaml:"write_timeout"`
	IdleTimeout      Duration `yaml:"idle_timeout"`
	ShutdownTimeout  Duration `yaml:"shutdown_timeout"`
	MaxBodyBytes     int64    `yaml:"max_body_bytes"`
	WatchdogInterval Duration `yaml:"watchdog_interval"`
	TLS              TLS      `yaml:"tls"`
}

// TLS configures the public listener certificate.
type TLS struct {
	Enabled  bool   `yaml:"enabled"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

// Paths are on-disk locations. The state directory must be mode 0700 in production.
type Paths struct {
	DataDir    string `yaml:"data_dir"`
	DBFile     string `yaml:"db_file"`
	ArchiveDir string `yaml:"archive_dir"`
	TmpDir     string `yaml:"tmp_dir"`
	BackupDir  string `yaml:"backup_dir"`
}

// Log configures stdout JSON logs and optional syslog.
type Log struct {
	Level  string `yaml:"level"`
	Syslog Syslog `yaml:"syslog"`
}

// Syslog is the optional second destination. Facility mail is the default.
type Syslog struct {
	Enabled  bool   `yaml:"enabled"`
	Network  string `yaml:"network"`
	Address  string `yaml:"address"`
	Facility string `yaml:"facility"`
	Tag      string `yaml:"tag"`
}

// Metrics toggles the Prometheus exposition on the admin listener.
type Metrics struct {
	Enabled bool `yaml:"enabled"`
}

// Queue controls workers, leases, and backoff.
type Queue struct {
	Workers           int      `yaml:"workers"`
	PollInterval      Duration `yaml:"poll_interval"`
	Lease             Duration `yaml:"lease"`
	MaxAttempts       int      `yaml:"max_attempts"`
	BackoffInitial    Duration `yaml:"backoff_initial"`
	BackoffMax        Duration `yaml:"backoff_max"`
	BackoffMultiplier float64  `yaml:"backoff_multiplier"`
}

// Breaker is the per-provider circuit breaker.
type Breaker struct {
	FailureThreshold int      `yaml:"failure_threshold"`
	Cooldown         Duration `yaml:"cooldown"`
}

// Spam configures Ollama, OpenRouter, and the timeout policy.
type Spam struct {
	Enabled    bool       `yaml:"enabled"`
	Mode       string     `yaml:"mode"`
	Timeout    Duration   `yaml:"timeout"`
	OnTimeout  string     `yaml:"on_timeout"`
	OnError    string     `yaml:"on_error"`
	Threshold  float64    `yaml:"threshold"`
	Ollama     Ollama     `yaml:"ollama"`
	OpenRouter OpenRouter `yaml:"openrouter"`
}

// Ollama is the preferred local classifier.
type Ollama struct {
	BaseURL string   `yaml:"base_url"`
	Model   string   `yaml:"model"`
	Timeout Duration `yaml:"timeout"`
}

// OpenRouter is the failover classifier.
type OpenRouter struct {
	BaseURL string   `yaml:"base_url"`
	Model   string   `yaml:"model"`
	Timeout Duration `yaml:"timeout"`
}

// Providers lists named delivery backends.
type Providers struct {
	Default string              `yaml:"default"`
	Items   map[string]Provider `yaml:"items"`
}

// Provider is one named backend. Unused fields depend on Type.
type Provider struct {
	Type                   string   `yaml:"type"`
	Fallback               string   `yaml:"fallback"`
	Timeout                Duration `yaml:"timeout"`
	Host                   string   `yaml:"host"`
	Port                   int      `yaml:"port"`
	TLSMode                string   `yaml:"tls_mode"`
	TLSInsecureSkipVerify  bool     `yaml:"tls_insecure_skip_verify"`
	Username               string   `yaml:"username"`
	HELO                   string   `yaml:"helo"`
	BaseURL                string   `yaml:"base_url"`
	Domain                 string   `yaml:"domain"`
	Region                 string   `yaml:"region"`
	TokenURL               string   `yaml:"token_url"`
	SendAs                 string   `yaml:"send_as"`
	Mode                   string   `yaml:"mode"`
	FailTimes              int      `yaml:"fail_times"`
}

// Client is a public-API caller identified by a key id.
type Client struct {
	AllowedFrom      []string  `yaml:"allowed_from"`
	AllowedProviders []string  `yaml:"allowed_providers"`
	MaxRecipients    int       `yaml:"max_recipients"`
	MaxMessageBytes  int64     `yaml:"max_message_bytes"`
	MaxAttachments   int       `yaml:"max_attachments"`
	RateLimit        RateLimit `yaml:"rate_limit"`
	WebhookURL       string    `yaml:"webhook_url"`
}

// RateLimit is a token bucket.
type RateLimit struct {
	RPS   float64 `yaml:"rps"`
	Burst int     `yaml:"burst"`
}

// Archive toggles RFC822 retention. Files are always written when enabled.
type Archive struct {
	Enabled bool `yaml:"enabled"`
}

// Idempotency is how long a key is remembered.
type Idempotency struct {
	TTL Duration `yaml:"ttl"`
}

// Security covers HMAC and the admin network policy.
type Security struct {
	HMACWindow     Duration `yaml:"hmac_window"`
	NonceTTL       Duration `yaml:"nonce_ttl"`
	AdminAllowCIDR []string `yaml:"admin_allow_cidrs"`
}

// Dedupe collapses identical bodies inside a window.
type Dedupe struct {
	Enabled bool     `yaml:"enabled"`
	Window  Duration `yaml:"window"`
}

// Backup runs VACUUM INTO on an interval.
type Backup struct {
	Enabled  bool     `yaml:"enabled"`
	Interval Duration `yaml:"interval"`
}

// TestUI serves the admin test page for one client key.
type TestUI struct {
	Enabled     bool   `yaml:"enabled"`
	ClientKeyID string `yaml:"client_key_id"`
}

// Secrets holds credentials. It is loaded from a separate file.
type Secrets struct {
	Admin     AdminSecret               `yaml:"admin"`
	Clients   map[string]ClientSecret   `yaml:"clients"`
	Providers map[string]ProviderSecret `yaml:"providers"`
	Spam      SpamSecret                `yaml:"spam"`
}

// AdminSecret is the bearer token for the admin API.
type AdminSecret struct {
	Token string `yaml:"token"`
}

// ClientSecret holds the HMAC secret and optional webhook secret.
type ClientSecret struct {
	HMACSecret    string `yaml:"hmac_secret"`
	WebhookSecret string `yaml:"webhook_secret"`
}

// ProviderSecret holds backend credentials. Only the fields for that type are required.
type ProviderSecret struct {
	Password     string `yaml:"password"`
	APIKey       string `yaml:"api_key"`
	ServerToken  string `yaml:"server_token"`
	AccessKey    string `yaml:"access_key"`
	SecretKey    string `yaml:"secret_key"`
	TenantID     string `yaml:"tenant_id"`
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
	RefreshToken string `yaml:"refresh_token"`
}

// SpamSecret holds the OpenRouter API key.
type SpamSecret struct {
	OpenRouter OpenRouterSecret `yaml:"openrouter"`
}

// OpenRouterSecret is the bearer token for OpenRouter.
type OpenRouterSecret struct {
	APIKey string `yaml:"api_key"`
}

// Snapshot is an immutable view swapped on successful reload.
type Snapshot struct {
	Config  Config
	Secrets Secrets
}

// Load reads a config file, applies defaults, and returns it unvalidated.
func Load(path string) (Config, error) {
	var cfg Config
	if err := loadYAML(path, &cfg); err != nil {
		return Config{}, err
	}
	ApplyDefaults(&cfg)
	return cfg, nil
}

// LoadSecrets reads the secrets file.
func LoadSecrets(path string) (Secrets, error) {
	var sec Secrets
	if err := loadYAML(path, &sec); err != nil {
		return Secrets{}, err
	}
	if sec.Clients == nil {
		sec.Clients = map[string]ClientSecret{}
	}
	if sec.Providers == nil {
		sec.Providers = map[string]ProviderSecret{}
	}
	return sec, nil
}

func loadYAML(path string, dest any) error {
	buf, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	dec := yaml.NewDecoder(strings.NewReader(string(buf)))
	dec.KnownFields(true)
	if err := dec.Decode(dest); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

// ApplyDefaults fills zero values. It does not invent providers or clients.
func ApplyDefaults(c *Config) {
	if c.Server.ReadTimeout == 0 {
		c.Server.ReadTimeout = Duration(15 * time.Second)
	}
	if c.Server.WriteTimeout == 0 {
		c.Server.WriteTimeout = Duration(30 * time.Second)
	}
	if c.Server.IdleTimeout == 0 {
		c.Server.IdleTimeout = Duration(60 * time.Second)
	}
	if c.Server.ShutdownTimeout == 0 {
		c.Server.ShutdownTimeout = Duration(20 * time.Second)
	}
	if c.Server.MaxBodyBytes == 0 {
		c.Server.MaxBodyBytes = 25 << 20
	}
	if c.Server.WatchdogInterval == 0 {
		c.Server.WatchdogInterval = Duration(10 * time.Second)
	}
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
	if c.Log.Syslog.Network == "" {
		c.Log.Syslog.Network = "unix"
	}
	if c.Log.Syslog.Address == "" {
		c.Log.Syslog.Address = "/dev/log"
	}
	if c.Log.Syslog.Facility == "" {
		c.Log.Syslog.Facility = "mail"
	}
	if c.Log.Syslog.Tag == "" {
		c.Log.Syslog.Tag = "mcon"
	}
	if c.Queue.Workers == 0 {
		c.Queue.Workers = 2
	}
	if c.Queue.PollInterval == 0 {
		c.Queue.PollInterval = Duration(500 * time.Millisecond)
	}
	if c.Queue.Lease == 0 {
		c.Queue.Lease = Duration(60 * time.Second)
	}
	if c.Queue.MaxAttempts == 0 {
		c.Queue.MaxAttempts = 8
	}
	if c.Queue.BackoffInitial == 0 {
		c.Queue.BackoffInitial = Duration(5 * time.Second)
	}
	if c.Queue.BackoffMax == 0 {
		c.Queue.BackoffMax = Duration(time.Hour)
	}
	if c.Queue.BackoffMultiplier == 0 {
		c.Queue.BackoffMultiplier = 2
	}
	if c.CircuitBreaker.FailureThreshold == 0 {
		c.CircuitBreaker.FailureThreshold = 5
	}
	if c.CircuitBreaker.Cooldown == 0 {
		c.CircuitBreaker.Cooldown = Duration(30 * time.Second)
	}
	if c.Spam.Mode == "" {
		c.Spam.Mode = "failover"
	}
	if c.Spam.Timeout == 0 {
		c.Spam.Timeout = Duration(8 * time.Second)
	}
	if c.Spam.OnTimeout == "" {
		c.Spam.OnTimeout = "send"
	}
	if c.Spam.OnError == "" {
		c.Spam.OnError = "send"
	}
	if c.Spam.Threshold == 0 {
		c.Spam.Threshold = 0.8
	}
	if c.Spam.Ollama.BaseURL == "" {
		c.Spam.Ollama.BaseURL = "http://127.0.0.1:11434"
	}
	if c.Spam.Ollama.Model == "" {
		c.Spam.Ollama.Model = "llama3.2"
	}
	if c.Spam.Ollama.Timeout == 0 {
		c.Spam.Ollama.Timeout = Duration(6 * time.Second)
	}
	if c.Spam.OpenRouter.BaseURL == "" {
		c.Spam.OpenRouter.BaseURL = "https://openrouter.ai/api/v1"
	}
	if c.Spam.OpenRouter.Model == "" {
		c.Spam.OpenRouter.Model = "openai/gpt-4o-mini"
	}
	if c.Spam.OpenRouter.Timeout == 0 {
		c.Spam.OpenRouter.Timeout = Duration(8 * time.Second)
	}
	if c.Idempotency.TTL == 0 {
		c.Idempotency.TTL = Duration(24 * time.Hour)
	}
	if c.Security.HMACWindow == 0 {
		c.Security.HMACWindow = Duration(5 * time.Minute)
	}
	if c.Security.NonceTTL == 0 {
		c.Security.NonceTTL = Duration(10 * time.Minute)
	}
	if len(c.Security.AdminAllowCIDR) == 0 {
		c.Security.AdminAllowCIDR = []string{"127.0.0.1/32", "::1/128"}
	}
	if c.Dedupe.Window == 0 {
		c.Dedupe.Window = Duration(10 * time.Minute)
	}
	if c.Backup.Interval == 0 {
		c.Backup.Interval = Duration(24 * time.Hour)
	}
	if c.Clients == nil {
		c.Clients = map[string]Client{}
	}
	if c.Providers.Items == nil {
		c.Providers.Items = map[string]Provider{}
	}
	for name, cl := range c.Clients {
		if cl.MaxRecipients == 0 {
			cl.MaxRecipients = 20
		}
		if cl.MaxMessageBytes == 0 {
			cl.MaxMessageBytes = 10 << 20
		}
		if cl.MaxAttachments == 0 {
			cl.MaxAttachments = 10
		}
		if cl.RateLimit.RPS == 0 {
			cl.RateLimit.RPS = 5
		}
		if cl.RateLimit.Burst == 0 {
			cl.RateLimit.Burst = 10
		}
		c.Clients[name] = cl
	}
	for name, p := range c.Providers.Items {
		if p.Timeout == 0 {
			p.Timeout = Duration(30 * time.Second)
		}
		if p.HELO == "" && p.Type == "smtp" {
			p.HELO = "mcon.local"
		}
		if p.Port == 0 && p.Type == "smtp" {
			p.Port = 25
		}
		if p.TLSMode == "" && p.Type == "smtp" {
			p.TLSMode = "none"
		}
		if p.Mode == "" && p.Type == "discard" {
			p.Mode = "success"
		}
		switch p.Type {
		case "sendgrid":
			if p.BaseURL == "" {
				p.BaseURL = "https://api.sendgrid.com"
			}
		case "mailgun":
			if p.BaseURL == "" {
				p.BaseURL = "https://api.mailgun.net"
			}
		case "postmark":
			if p.BaseURL == "" {
				p.BaseURL = "https://api.postmarkapp.com"
			}
		case "brevo":
			if p.BaseURL == "" {
				p.BaseURL = "https://api.brevo.com"
			}
		case "resend":
			if p.BaseURL == "" {
				p.BaseURL = "https://api.resend.com"
			}
		case "graph":
			if p.BaseURL == "" {
				p.BaseURL = "https://graph.microsoft.com/v1.0"
			}
		case "gmail":
			if p.BaseURL == "" {
				p.BaseURL = "https://gmail.googleapis.com"
			}
			if p.TokenURL == "" {
				p.TokenURL = "https://oauth2.googleapis.com/token"
			}
		case "ses":
			if p.Region == "" {
				p.Region = "eu-central-1"
			}
		}
		c.Providers.Items[name] = p
	}
}

// Validate checks structure and the plaintext-SMTP loopback rule.
func Validate(c Config, s Secrets) error {
	var problems []error
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Errorf(format, args...))
	}
	if strings.TrimSpace(c.Server.PublicAddr) == "" {
		add("server.public_addr is required")
	}
	if strings.TrimSpace(c.Server.AdminAddr) == "" {
		add("server.admin_addr is required")
	} else if !addrLoopback(c.Server.AdminAddr) {
		add("server.admin_addr must bind to a loopback address")
	}
	if !addrLoopback(c.Server.PublicAddr) && !c.Server.TLS.Enabled {
		add("server.tls.enabled is required when public_addr is not loopback")
	}
	if c.Server.TLS.Enabled {
		if strings.TrimSpace(c.Server.TLS.CertFile) == "" || strings.TrimSpace(c.Server.TLS.KeyFile) == "" {
			add("server.tls.cert_file and key_file are required when tls is enabled")
		}
	}
	if c.Server.MaxBodyBytes < 1024 {
		add("server.max_body_bytes must be at least 1024")
	}
	for _, p := range []struct {
		name string
		d    Duration
	}{
		{"server.read_timeout", c.Server.ReadTimeout},
		{"server.write_timeout", c.Server.WriteTimeout},
		{"server.idle_timeout", c.Server.IdleTimeout},
		{"server.shutdown_timeout", c.Server.ShutdownTimeout},
		{"server.watchdog_interval", c.Server.WatchdogInterval},
	} {
		if p.d.Std() <= 0 {
			add("%s must be positive", p.name)
		}
	}
	if c.Paths.DataDir == "" || c.Paths.DBFile == "" || c.Paths.ArchiveDir == "" || c.Paths.TmpDir == "" || c.Paths.BackupDir == "" {
		add("paths.data_dir, db_file, archive_dir, tmp_dir, and backup_dir are required")
	}
	switch strings.ToLower(c.Log.Level) {
	case "debug", "info", "warn", "warning", "error":
	default:
		add("log.level must be debug, info, warn, or error")
	}
	switch c.Log.Syslog.Facility {
	case "mail", "user", "daemon", "local0", "local1", "local2", "local3", "local4", "local5", "local6", "local7":
	default:
		add("log.syslog.facility is not supported")
	}
	if c.Queue.Workers < 1 {
		add("queue.workers must be >= 1")
	}
	if c.Queue.MaxAttempts < 1 {
		add("queue.max_attempts must be >= 1")
	}
	if c.Queue.PollInterval.Std() <= 0 || c.Queue.Lease.Std() <= 0 || c.Queue.BackoffInitial.Std() <= 0 || c.Queue.BackoffMax.Std() <= 0 {
		add("queue intervals must be positive")
	}
	if c.Queue.BackoffMultiplier < 1 {
		add("queue.backoff_multiplier must be >= 1")
	}
	if c.Queue.BackoffMax.Std() < c.Queue.BackoffInitial.Std() {
		add("queue.backoff_max must be >= backoff_initial")
	}
	if c.CircuitBreaker.FailureThreshold < 1 || c.CircuitBreaker.Cooldown.Std() <= 0 {
		add("circuit_breaker threshold and cooldown must be positive")
	}
	switch c.Spam.Mode {
	case "failover", "ollama", "openrouter":
	default:
		add("spam.mode must be failover, ollama, or openrouter")
	}
	if c.Spam.OnTimeout != "send" && c.Spam.OnTimeout != "block" {
		add("spam.on_timeout must be send or block")
	}
	if c.Spam.OnError != "send" && c.Spam.OnError != "block" {
		add("spam.on_error must be send or block")
	}
	if c.Spam.Threshold < 0 || c.Spam.Threshold > 1 {
		add("spam.threshold must be between 0 and 1")
	}
	if c.Spam.Timeout.Std() <= 0 || c.Spam.Ollama.Timeout.Std() <= 0 || c.Spam.OpenRouter.Timeout.Std() <= 0 {
		add("spam timeouts must be positive")
	}
	if c.Spam.Enabled && (c.Spam.Mode == "failover" || c.Spam.Mode == "ollama") {
		if strings.TrimSpace(c.Spam.Ollama.BaseURL) == "" || strings.TrimSpace(c.Spam.Ollama.Model) == "" {
			add("spam.ollama.base_url and model are required")
		}
	}
	if c.Spam.Enabled && (c.Spam.Mode == "failover" || c.Spam.Mode == "openrouter") {
		if strings.TrimSpace(s.Spam.OpenRouter.APIKey) == "" {
			add("secrets spam.openrouter.api_key is required when openrouter is used")
		}
		if strings.TrimSpace(c.Spam.OpenRouter.Model) == "" {
			add("spam.openrouter.model is required")
		}
	}
	if len(c.Providers.Items) == 0 {
		add("at least one provider is required")
	}
	if _, ok := c.Providers.Items[c.Providers.Default]; !ok {
		add("providers.default %q is not defined", c.Providers.Default)
	}
	for name, p := range c.Providers.Items {
		if !validName(name) {
			add("provider name %q is invalid", name)
		}
		if p.Fallback != "" {
			if p.Fallback == name {
				add("provider %s fallback must not be itself", name)
			}
			if _, ok := c.Providers.Items[p.Fallback]; !ok {
				add("provider %s fallback %q is not defined", name, p.Fallback)
			}
		}
		if p.Timeout.Std() <= 0 {
			add("provider %s timeout must be positive", name)
		}
		sec := s.Providers[name]
		switch p.Type {
		case "smtp":
			if p.Host == "" || p.Port < 1 || p.Port > 65535 {
				add("provider %s smtp host and port are required", name)
			}
			switch p.TLSMode {
			case "none", "starttls", "tls":
			default:
				add("provider %s tls_mode must be none, starttls, or tls", name)
			}
			if p.TLSMode == "none" && !hostLoopback(p.Host) {
				add("provider %s plaintext SMTP is only allowed to a loopback host", name)
			}
			if p.Username != "" && strings.TrimSpace(sec.Password) == "" {
				add("provider %s requires secrets.providers.%s.password", name, name)
			}
			if p.Username == "" && sec.Password != "" {
				add("provider %s password is set but username is empty", name)
			}
		case "sendgrid", "brevo", "resend":
			if strings.TrimSpace(sec.APIKey) == "" {
				add("provider %s requires an api_key", name)
			}
		case "mailgun":
			if strings.TrimSpace(sec.APIKey) == "" || strings.TrimSpace(p.Domain) == "" {
				add("provider %s requires api_key and domain", name)
			}
		case "postmark":
			if strings.TrimSpace(sec.ServerToken) == "" {
				add("provider %s requires server_token", name)
			}
		case "ses":
			if strings.TrimSpace(sec.AccessKey) == "" || strings.TrimSpace(sec.SecretKey) == "" || strings.TrimSpace(p.Region) == "" {
				add("provider %s requires access_key, secret_key, and region", name)
			}
		case "graph":
			if strings.TrimSpace(sec.TenantID) == "" || strings.TrimSpace(sec.ClientID) == "" || strings.TrimSpace(sec.ClientSecret) == "" || strings.TrimSpace(p.SendAs) == "" {
				add("provider %s requires tenant_id, client_id, client_secret, and send_as", name)
			}
		case "gmail":
			if strings.TrimSpace(sec.ClientID) == "" || strings.TrimSpace(sec.ClientSecret) == "" || strings.TrimSpace(sec.RefreshToken) == "" {
				add("provider %s requires client_id, client_secret, and refresh_token", name)
			}
		case "discard":
			switch p.Mode {
			case "success", "transient", "permanent":
			default:
				add("provider %s discard mode must be success, transient, or permanent", name)
			}
			if p.FailTimes < 0 {
				add("provider %s fail_times must be >= 0", name)
			}
		default:
			add("provider %s has unknown type %q", name, p.Type)
		}
	}
	if len(c.Clients) == 0 {
		add("at least one client is required")
	}
	if len(strings.TrimSpace(s.Admin.Token)) < 16 {
		add("secrets admin.token must be at least 16 characters")
	}
	for name, cl := range c.Clients {
		if !validName(name) {
			add("client name %q is invalid", name)
		}
		sec, ok := s.Clients[name]
		if !ok || len(strings.TrimSpace(sec.HMACSecret)) < 16 {
			add("client %s requires secrets.clients.%s.hmac_secret of at least 16 characters", name, name)
		}
		if len(cl.AllowedFrom) == 0 {
			add("client %s allowed_from must not be empty", name)
		}
		for _, from := range cl.AllowedFrom {
			if !strings.Contains(from, "@") || strings.ContainsAny(from, "\r\n ") {
				add("client %s allowed_from %q is invalid", name, from)
			}
		}
		for _, pname := range cl.AllowedProviders {
			if _, ok := c.Providers.Items[pname]; !ok {
				add("client %s allowed provider %q is not defined", name, pname)
			}
		}
		if cl.MaxRecipients < 1 || cl.MaxAttachments < 0 || cl.MaxMessageBytes < 1 {
			add("client %s size limits are invalid", name)
		}
		if cl.RateLimit.RPS <= 0 || cl.RateLimit.Burst < 1 {
			add("client %s rate_limit is invalid", name)
		}
		if cl.WebhookURL != "" {
			u, err := url.Parse(cl.WebhookURL)
			if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
				add("client %s webhook_url must be an http(s) URL", name)
			}
			if len(strings.TrimSpace(sec.WebhookSecret)) < 16 {
				add("client %s webhook_url requires webhook_secret of at least 16 characters", name)
			}
		}
	}
	if c.TestUI.Enabled {
		if _, ok := c.Clients[c.TestUI.ClientKeyID]; !ok {
			add("test_ui.client_key_id %q is not a client", c.TestUI.ClientKeyID)
		}
	}
	if c.Idempotency.TTL.Std() <= 0 || c.Security.HMACWindow.Std() <= 0 || c.Security.NonceTTL.Std() <= 0 {
		add("idempotency and security windows must be positive")
	}
	if c.Dedupe.Enabled && c.Dedupe.Window.Std() <= 0 {
		add("dedupe.window must be positive")
	}
	if c.Backup.Enabled && c.Backup.Interval.Std() <= 0 {
		add("backup.interval must be positive")
	}
	if len(c.Security.AdminAllowCIDR) == 0 {
		add("security.admin_allow_cidrs must not be empty")
	}
	for _, cidr := range c.Security.AdminAllowCIDR {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			add("admin CIDR %q: %v", cidr, err)
		}
	}
	return errors.Join(problems...)
}

// Prepare creates state directories and checks that TLS files exist.
func Prepare(c Config) error {
	for _, dir := range []string{c.Paths.DataDir, c.Paths.ArchiveDir, c.Paths.TmpDir, c.Paths.BackupDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	if c.Server.TLS.Enabled {
		for _, f := range []string{c.Server.TLS.CertFile, c.Server.TLS.KeyFile} {
			if _, err := os.Stat(f); err != nil {
				return fmt.Errorf("tls file %s: %w", f, err)
			}
		}
	}
	return nil
}

// Local returns a valid loopback configuration that delivers to the discard provider.
// Callers use it in tests and local smoke runs.
func Local(dir string) (Config, Secrets) {
	cfg := Config{
		Server: Server{
			PublicAddr: "127.0.0.1:0",
			AdminAddr:  "127.0.0.1:0",
			TLS:        TLS{Enabled: false},
		},
		Paths: Paths{
			DataDir:    dir,
			DBFile:     filepath.Join(dir, "mcon.db"),
			ArchiveDir: filepath.Join(dir, "archive"),
			TmpDir:     filepath.Join(dir, "tmp"),
			BackupDir:  filepath.Join(dir, "backups"),
		},
		Spam: Spam{Enabled: false, Mode: "ollama", OnTimeout: "send", OnError: "send", Threshold: 0.8},
		Providers: Providers{
			Default: "discard",
			Items: map[string]Provider{
				"discard": {Type: "discard", Mode: "success"},
			},
		},
		Clients: map[string]Client{
			"website": {
				AllowedFrom: []string{"noreply@example.com"},
				WebhookURL:  "",
			},
		},
		Archive: Archive{Enabled: true},
		Dedupe:  Dedupe{Enabled: true},
		Backup:  Backup{Enabled: false},
		TestUI:  TestUI{Enabled: true, ClientKeyID: "website"},
		Metrics: Metrics{Enabled: true},
	}
	ApplyDefaults(&cfg)
	sec := Secrets{
		Admin: AdminSecret{Token: "test-admin-token-value"},
		Clients: map[string]ClientSecret{
			"website": {HMACSecret: "test-hmac-secret-value"},
		},
		Providers: map[string]ProviderSecret{},
	}
	return cfg, sec
}

func validName(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}

func addrLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	return hostLoopback(host)
}

func hostLoopback(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
