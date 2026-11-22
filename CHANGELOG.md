# Changelog

All notable changes to M-Con are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-11-22

### Added

- Initial M-Con mail service as a single Go binary for systemd.
- Public HMAC accept API and localhost admin API (health, metrics, reload, resend, replay).
- SQLite queue with idempotency, retries/backoff, and on-disk archive.
- Provider adapters: SMTP, SendGrid, Mailgun, Postmark, Resend, Brevo, SES, Gmail, Microsoft Graph, HTTP, discard.
- Optional spam pipeline (heuristics, Ollama, OpenRouter failover) and signed outbound webhooks.
- `mcon-ctl` CLI (`send`, `status --wait`, `resend`, `replay`, `health`, `version`).
- Admin test page under `/test/`, example configs, systemd unit, and docs (API, CONFIG, ARCHITECTURE, OPERATIONS, TESTING).
- SemVer via `VERSION`, Conventional Commits hook, and GitHub Actions CI.
