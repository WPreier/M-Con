# Changelog

All notable changes to M-Con are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.5] - 2026-11-25

### Added

- SMTP option `tls_insecure_skip_verify` to skip certificate verification for internal/self-signed/expired relay certs (`starttls` and `tls`).

## [0.1.4] - 2026-11-25

### Changed

- Local mail test uses dedicated ports `18687`/`18688` so it does not collide with a TLS production listener on `:8687`.
- Documented and detected the HTTP-vs-HTTPS mismatch (`Client sent an HTTP request to an HTTPS server`) in TESTING.md and `scripts/local-mail-test.sh`.

## [0.1.3] - 2026-11-25

### Added

- Local real-mail test harness: `configs/local/*.example` and `scripts/local-mail-test.sh` (SMTP Echtversand, Test-UI, documented in TESTING.md).
- Client configuration docs: `website` is a placeholder id, `allowed_from` requires full email addresses, `test_ui.client_key_id` must match.

### Changed

- INSTALL: start in `/usr/local/src`, expanded TLS section (Let's Encrypt copy vs symlink, `0640 root:mcon`, permission-denied troubleshooting).
- `mcon-ctl` accepts flags before or after the subcommand (e.g. `-config … health`).
- Mark `scripts/smoke.sh`, `scripts/bump-version.sh`, and `scripts/local-mail-test.sh` executable in git (`100755`).

## [0.1.2] - 2026-11-23

### Fixed

- Smoke test now asserts against `version.Version` instead of a hardcoded `0.1.0`, so CI stays green after patch bumps.

## [0.1.1] - 2026-11-23

### Added

- Detailed installation guide in `docs/INSTALL.md` (build, systemd paths, checklist, troubleshooting).

### Changed

- Documented production secrets path `/etc/mcon/secrets.yaml` (`0440`, `root:mcon`) in INSTALL, CONFIG, OPERATIONS, README, and the secrets example.

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
