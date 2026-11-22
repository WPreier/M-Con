package main

import (
	"fmt"
	"io"
	"os"

	"mcon/internal/app"
	"mcon/internal/config"
	"mcon/internal/version"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flagSet(stderr)
	configPath := fs.String("config", os.Getenv("MCON_CONFIG"), "path to mcon.yaml")
	secretsPath := fs.String("secrets", os.Getenv("MCON_SECRETS"), "path to secrets.yaml")
	showVersion := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Fprintln(stdout, version.Version)
		return 0
	}
	if *configPath == "" || *secretsPath == "" {
		fmt.Fprintln(stderr, "mcon: config and secrets are required")
		return 1
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	sec, err := config.LoadSecrets(*secretsPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	application, err := app.Open(cfg, sec, *configPath, *secretsPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ctx, cancel := notifyContext()
	defer cancel()
	if err := application.Run(ctx); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
