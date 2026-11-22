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

## Versionierung

`scripts/hooks/commit-msg` prüft Conventional Commits gegen `VERSION` und den Changelog. Aktivieren, ohne das Repo zu verändern, mit `git config core.hooksPath scripts/hooks`. `scripts/bump-version.sh patch|minor|major` schreibt die nächste Nummer.

Die CI-Workflow-Datei `.github/workflows/ci.yml` führt `go test ./...` aus und prüft, dass `mcon -version` der Datei `VERSION` entspricht.
