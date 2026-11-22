# Architektur

M-Con nimmt Kontaktformular-Mails an, prüft sie und stellt sie über austauschbare Provider zu. Es gibt zwei Binaries:

- `mcon` ist der systemd-Dienst (`Type=notify`).
- `mcon-ctl` spricht die HTTP-APIs an.

## Laufzeit

Die öffentliche API lauscht mit TLS (Standard `:8687`). Die Admin-API bleibt auf dem Loopback (`127.0.0.1:8688`) und prüft zusätzlich eine CIDR-Liste. Beide Prozesse teilen sich eine SQLite-Datei im WAL-Modus mit `synchronous=FULL`.

Eine Nachricht wird erst mit HTTP 202 beantwortet, wenn die Zeile in SQLite und die `.eml`-Datei im Archiv geschrieben sind. Der Worker pollt danach asynchron.

## Zustände

`received` → `spam_check` → `queued` oder `blocked` → `sending` → `sent`, `retry_wait` oder `dead`.

`cancelled` ist der Abbruch vor dem Versand. Ein Resend legt eine neue ULID an und setzt `resend_of`. Die neue Nachricht behält `body_sha256`. Ein Replay setzt eine `dead`-Nachricht wieder auf `queued`.

Der Worker least eine Nachricht (`lease_until`). Läuft der Prozess während des Versands ab, übernimmt ein späterer Poll die Zeile erneut. Zustellung ist daher at-least-once; die RFC-Message-ID und `X-MCon-Id` bleiben über Retries stabil.

## Bausteine

| Paket | Aufgabe |
|---|---|
| `internal/config` | YAML, Defaults, Validierung. Klartext-SMTP nur zu Loopback. |
| `internal/store` | Schema, Migrationen, Queue, Idempotenz, Nonces, Outbox. |
| `internal/auth` | HMAC-SHA256, Nonce, Token-Bucket. |
| `internal/message` | Validierung, Body-Hash, RFC822. |
| `internal/archive` | `.eml` unter `YYYY/MM/`. |
| `internal/spam` | Ollama, OpenRouter-Failover, Timeout-Policy. |
| `internal/provider` | Einheitliches `Send`, Circuit Breaker, ein Fallback. |
| `internal/queue` | Claim, Backoff, Dead-Letter. |
| `internal/webhook` | Signierte Outbox. |
| `internal/httpapi` | Public- und Admin-HTTP. |
| `web/test` | Testseite, serverseitig in die Admin-API eingebettet. |

## Spam

Anhänge gehen nicht an das Modell. Der Prompt verlangt JSON `{spam, score, reason}`. Modus `failover` ruft zuerst Ollama und bei Fehler oder Timeout OpenRouter. `on_timeout` und `on_error` sind `send` oder `block`. Ein Score ab `threshold` blockiert auch dann, wenn das Modell `spam: false` sagt.

## Provider

SMTP, SendGrid, Mailgun, SES (SigV4, Raw), Postmark, Brevo, Resend, Microsoft Graph, Gmail und `discard`. Transiente Fehler und ein offener Circuit Breaker lösen höchstens einen Fallback aus. Permanente Fehler (SMTP 5xx, die meisten HTTP 4xx) landen direkt in `dead`.

SPF, DKIM und DMARC bleiben am MTA bzw. im DNS.

## Version

`VERSION` ist SemVer. `scripts/hooks/commit-msg` verlangt bei `fix:` einen Patch, bei `feat:` einen Minor und bei `BREAKING CHANGE` oder `type!:` einen Major, jeweils inklusive Changelog-Abschnitt.
