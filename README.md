# M-Con

M-Con nimmt Kontaktformulare per HMAC-API an, prüft sie optional auf Spam und stellt sie über SMTP oder einen HTTP-/OAuth-Provider zu. Queue, Idempotenz und Archiv liegen in SQLite. Der Dienst ist ein einzelnes Go-Binary für systemd.

## Bestandteile

- `mcon` — Daemon (Public `:8687`, Admin `127.0.0.1:8688`)
- `mcon-ctl` — `send`, `status --wait`, `resend`, `replay`, `health`, `version`
- `web/test/` — Testseite unter `/test/` auf dem Admin-Port
- `configs/*.example` — Konfiguration ohne echte Secrets

## Start

```
go build -ldflags "-X mcon/internal/version.Version=$(cat VERSION)" -o mcon ./cmd/mcon
./mcon -config /etc/mcon/mcon.yaml -secrets /etc/mcon/secrets.yaml
```

Ohne `-config` und `-secrets` endet der Prozess mit Status 1. `-version` gibt die SemVer aus.

Installation Schritt für Schritt: [docs/INSTALL.md](docs/INSTALL.md) (`secrets.yaml` unter `/etc/mcon/secrets.yaml`). Betrieb und Signale: [docs/OPERATIONS.md](docs/OPERATIONS.md). Schnittstelle, Konfiguration, Architektur und Tests: [docs/API.md](docs/API.md), [docs/CONFIG.md](docs/CONFIG.md), [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md), [docs/TESTING.md](docs/TESTING.md).

## Tests

```
go test ./...
```

Lokaler Echtversand per SMTP: [docs/TESTING.md](docs/TESTING.md) und `scripts/local-mail-test.sh`.

Version `0.1.3`. Conventional Commits (`feat`, `fix`, `BREAKING CHANGE`) prüft `scripts/hooks/commit-msg`.
