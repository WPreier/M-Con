package app

import (
	"net/http"

	"mcon/internal/config"
	"mcon/internal/provider"
	"mcon/internal/provider/brevo"
	"mcon/internal/provider/discard"
	"mcon/internal/provider/gmail"
	"mcon/internal/provider/graph"
	"mcon/internal/provider/mailgun"
	"mcon/internal/provider/postmark"
	"mcon/internal/provider/resend"
	"mcon/internal/provider/sendgrid"
	"mcon/internal/provider/ses"
	"mcon/internal/provider/smtp"
)

func buildRegistry(cfg config.Config, sec config.Secrets) *provider.Registry {
	reg := provider.NewRegistry(cfg.CircuitBreaker.FailureThreshold, cfg.CircuitBreaker.Cooldown.Std())
	client := &http.Client{Timeout: 0}
	for name, item := range cfg.Providers.Items {
		secret := sec.Providers[name]
		timeout := item.Timeout.Std()
		var sender provider.Sender
		switch item.Type {
		case "discard":
			sender = &discard.Provider{Mode: item.Mode, FailTimes: item.FailTimes}
		case "smtp":
			sender = &smtp.Client{Host: item.Host, Port: item.Port, TLSMode: item.TLSMode, Username: item.Username, Password: secret.Password, HELO: item.HELO, Timeout: timeout}
		case "sendgrid":
			sender = &sendgrid.Client{BaseURL: item.BaseURL, APIKey: secret.APIKey, Timeout: timeout, HTTP: client}
		case "mailgun":
			sender = &mailgun.Client{BaseURL: item.BaseURL, Domain: item.Domain, APIKey: secret.APIKey, Timeout: timeout, HTTP: client}
		case "ses":
			sender = &ses.Client{Region: item.Region, AccessKey: secret.AccessKey, SecretKey: secret.SecretKey, Timeout: timeout, HTTP: client}
		case "postmark":
			sender = &postmark.Client{BaseURL: item.BaseURL, ServerToken: secret.ServerToken, Timeout: timeout, HTTP: client}
		case "brevo":
			sender = &brevo.Client{BaseURL: item.BaseURL, APIKey: secret.APIKey, Timeout: timeout, HTTP: client}
		case "resend":
			sender = &resend.Client{BaseURL: item.BaseURL, APIKey: secret.APIKey, Timeout: timeout, HTTP: client}
		case "graph":
			sender = &graph.Client{BaseURL: item.BaseURL, TokenURL: item.TokenURL, TenantID: secret.TenantID, ClientID: secret.ClientID, ClientSecret: secret.ClientSecret, SendAs: item.SendAs, Timeout: timeout, HTTP: client}
		case "gmail":
			sender = &gmail.Client{BaseURL: item.BaseURL, TokenURL: item.TokenURL, ClientID: secret.ClientID, ClientSecret: secret.ClientSecret, RefreshToken: secret.RefreshToken, Timeout: timeout, HTTP: client}
		default:
			continue
		}
		reg.Set(name, sender, item.Fallback)
	}
	return reg
}
