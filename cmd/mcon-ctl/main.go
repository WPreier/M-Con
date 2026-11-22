package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"mcon/internal/ctl"
	"mcon/internal/version"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: mcon-ctl [--flags] send|status|resend|replay|health|version")
		return 2
	}
	if args[0] == "-version" || args[0] == "--version" {
		fmt.Println(version.Version)
		return 0
	}
	cmd := args[0]
	fs := flag.NewFlagSet("mcon-ctl", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configPath := fs.String("config", os.Getenv("MCON_CTL_CONFIG"), "client config file")
	publicURL := fs.String("public-url", os.Getenv("MCON_PUBLIC_URL"), "public API base URL")
	adminURL := fs.String("admin-url", os.Getenv("MCON_ADMIN_URL"), "admin API base URL")
	keyID := fs.String("key-id", os.Getenv("MCON_KEY_ID"), "client key id")
	secret := fs.String("hmac-secret", os.Getenv("MCON_HMAC_SECRET"), "HMAC secret")
	token := fs.String("admin-token", os.Getenv("MCON_ADMIN_TOKEN"), "admin bearer token")
	insecure := fs.Bool("insecure", false, "skip TLS verification")
	file := fs.String("file", "", "JSON message file for send")
	id := fs.String("id", "", "message id")
	wait := fs.Bool("wait", false, "poll status until a terminal state")
	timeout := fs.Duration("timeout", 30*time.Second, "wait timeout")
	skipSpam := fs.Bool("skip-spam", false, "resend without the spam check")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	opt := ctl.Options{Insecure: *insecure}
	if *configPath != "" {
		buf, err := os.ReadFile(*configPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		var fileOpt struct {
			PublicURL  string `yaml:"public_url"`
			AdminURL   string `yaml:"admin_url"`
			KeyID      string `yaml:"key_id"`
			HMACSecret string `yaml:"hmac_secret"`
			AdminToken string `yaml:"admin_token"`
			Insecure   bool   `yaml:"insecure_skip_verify"`
		}
		if err := yaml.Unmarshal(buf, &fileOpt); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		opt.PublicURL, opt.AdminURL, opt.KeyID = fileOpt.PublicURL, fileOpt.AdminURL, fileOpt.KeyID
		opt.HMACSecret, opt.AdminToken, opt.Insecure = fileOpt.HMACSecret, fileOpt.AdminToken, fileOpt.Insecure
	}
	if *publicURL != "" {
		opt.PublicURL = *publicURL
	}
	if *adminURL != "" {
		opt.AdminURL = *adminURL
	}
	if *keyID != "" {
		opt.KeyID = *keyID
	}
	if *secret != "" {
		opt.HMACSecret = *secret
	}
	if *token != "" {
		opt.AdminToken = *token
	}
	if *insecure {
		opt.Insecure = true
	}
	ctx := context.Background()
	var (
		out string
		err error
	)
	switch cmd {
	case "send":
		if *file == "" {
			fmt.Fprintln(os.Stderr, "send requires --file")
			return 2
		}
		body, rerr := os.ReadFile(*file)
		if rerr != nil {
			fmt.Fprintln(os.Stderr, rerr)
			return 1
		}
		out, err = ctl.Send(ctx, opt, body, "")
	case "status":
		if *wait {
			out, err = ctl.Wait(ctx, opt, *id, *timeout)
		} else {
			out, err = ctl.Status(ctx, opt, *id)
		}
	case "resend":
		out, err = ctl.Resend(ctx, opt, *id, *skipSpam)
	case "replay":
		out, err = ctl.Replay(ctx, opt, *id)
	case "health":
		out, err = ctl.Health(ctx, opt)
	case "version":
		out, err = ctl.Version(ctx, opt)
	default:
		fmt.Fprintln(os.Stderr, "unknown command", cmd)
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println(out)
	return 0
}
