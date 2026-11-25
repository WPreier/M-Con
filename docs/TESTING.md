# Tests

```
go test ./...
```

Die Suite läuft ohne CGO (`modernc.org/sqlite`) und ohne echte Provider. Abgedeckt sind:

- HMAC, Zeitfenster, Nonce-Replay und Token-Bucket
- Header-Injection, RFC822 ohne Bcc, stabile Body-Hashes
- SQLite-Migrationen, WAL und `synchronous=FULL`
- Idempotenz, Archiv vor der Antwort, Admin-Testseite
- Queue-Backoff, Claim/Lease, Dead-Letter und Replay gegen `discard`
- SMTP-Sink mit 250, 450 und 550
- HTTP- und OAuth-Adapter über `httptest` (SendGrid, Mailgun, SES, Postmark, Brevo, Resend, Graph, Gmail)
- Circuit Breaker und Fallback
- Spam-Matrix, Failover und Timeout
- signierte Webhook-Outbox
- Prozess-Smoke: annehmen, zustellen, `/version`
- Start ohne Config endet mit Status 1
- `VERSION` und `CHANGELOG.md` nennen dieselbe SemVer

Fixtures liegen in `testdata/` (`contact.json`, `spam_samples.json`, `note.txt`).

## Lokaler Echtversand

Zum Testen gegen einen echten SMTP-Relay (kein Unit-Test, echte Zustellung):

```bash
cd /usr/local/src/M-Con   # oder Repo-Root
cp configs/local/mcon.yaml.example configs/local/mcon.yaml
cp configs/local/secrets.yaml.example configs/local/secrets.yaml
cp configs/local/mcon-ctl.yaml.example configs/local/mcon-ctl.yaml
```

In den drei Dateien setzen:

| Datei | Pflicht |
|---|---|
| `mcon.yaml` | `providers.items.smtp-real.host` / `port` / `tls_mode` / `username`, `clients.local.allowed_from` (volle Absender-Adresse, die der Relay erlaubt) |
| `secrets.yaml` | `providers.smtp-real.password`, Tokens/HMAC (mind. 16 Zeichen) — gleiche Werte wie in `mcon-ctl.yaml` |
| `mcon-ctl.yaml` | `key_id: local`, passende `hmac_secret` / `admin_token` |

Dann:

```bash
# eine Testmail senden und mcon danach beenden
MCON_TEST_FROM='noreply@example.com' MCON_TEST_TO='du@example.com' \
  scripts/local-mail-test.sh

# mcon laufen lassen (Test-UI unter http://127.0.0.1:8688/test/)
scripts/local-mail-test.sh --daemon

# gegen laufenden lokalen Daemon nur senden
MCON_TEST_TO='du@example.com' scripts/local-mail-test.sh --send-only

# senden und Daemon danach weiterlaufen lassen
MCON_TEST_TO='du@example.com' scripts/local-mail-test.sh --keep
```

Das Skript baut `bin/mcon` und `bin/mcon-ctl`, legt State unter `./var/local/` ab (gitignored), startet auf **`127.0.0.1:18687` / `:18688`** ohne TLS (eigene Ports, damit der Produktionsdienst auf `:8687`/`:8688` nicht stört) und wartet auf `state=sent`.

Hinweise:

- `from` muss in `allowed_from` stehen und vom SMTP-Provider als Absender akzeptiert werden.
- Spam ist in der Local-Config aus (`spam.enabled: false`).
- Zum Debuggen ohne Zustellung `providers.default: discard` setzen.
- Nach dem Test Posteingang **und** Spam-Ordner prüfen; Provider-Fehler stehen in `var/local/mcon.log`.
- Test-UI: `http://127.0.0.1:18688/test/`

### Fehler: `Client sent an HTTP request to an HTTPS server`

`mcon-ctl` spricht **HTTP**, der Listener auf dem Port erwartet aber **HTTPS**. Typisch: Produktions-`mcon` mit TLS läuft noch auf `:8687`, und `public_url` zeigt dorthin (`http://127.0.0.1:8687`).

Fix:

1. Local-Ports nutzen (siehe `configs/local/*.example`):

```bash
# in configs/local/mcon.yaml
server.public_addr: "127.0.0.1:18687"
server.admin_addr:  "127.0.0.1:18688"
server.tls.enabled: false

# in configs/local/mcon-ctl.yaml
public_url: http://127.0.0.1:18687
admin_url:  http://127.0.0.1:18688
```

2. Oder bewusst gegen Produktion mit TLS testen:

```yaml
public_url: https://127.0.0.1:8687
admin_url: http://127.0.0.1:8688
insecure_skip_verify: true   # nur bei self-signed / lokalem Cert
```

Schema und Port müssen zum laufenden Listener passen (`http` ↔ `tls.enabled: false`, `https` ↔ `tls.enabled: true`).

## Versionierung

`scripts/hooks/commit-msg` prüft Conventional Commits gegen `VERSION` und den Changelog. Aktivieren, ohne das Repo zu verändern, mit `git config core.hooksPath scripts/hooks`. `scripts/bump-version.sh patch|minor|major` schreibt die nächste Nummer.

Die CI-Workflow-Datei `.github/workflows/ci.yml` führt `go test ./...` aus und prüft, dass `mcon -version` der Datei `VERSION` entspricht.
