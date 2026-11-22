# Betrieb

## Installation

Die vollständige Installationsanleitung steht in [INSTALL.md](INSTALL.md).

Kurz: Binaries nach `/usr/local/bin/`, Config und Secrets nach `/etc/mcon/` (`mcon.yaml`, **`secrets.yaml`**). Der Dienstbenutzer kommt aus `deploy/mcon.sysusers`, Verzeichnisse aus `deploy/mcon.tmpfiles`, Unit aus `deploy/mcon.service` (`Type=notify`, `WatchdogSec=30s`, `ProtectSystem=strict`, Schreibrecht nur auf `/var/lib/mcon`).

```
install -d -o mcon -g mcon -m 0700 /var/lib/mcon /var/lib/mcon/archive /var/lib/mcon/tmp /var/lib/mcon/backups
install -m 0755 mcon mcon-ctl /usr/local/bin/
# secrets: /etc/mcon/secrets.yaml (0440 root:mcon) — siehe INSTALL.md
systemctl enable --now mcon
```

Build mit der Versionsdatei:

```
v=$(tr -d ' \n' < VERSION)
go build -ldflags "-X mcon/internal/version.Version=$v" -o mcon ./cmd/mcon
go build -ldflags "-X mcon/internal/version.Version=$v" -o mcon-ctl ./cmd/mcon-ctl
```

## Logs und Metriken

Stdout ist JSON und geht ins Journal. Optional schreibt der Dienst zusätzlich ins Syslog mit Facility `mail` (`log.syslog`). Schlägt der Syslog-Dial fehl, startet der Prozess trotzdem. Bodies, Tokens und Secrets werden nicht geloggt; Schlüsselnamen mit secret, token, password oder hmac werden geschwärzt.

Prometheus-Text liegt auf dem Admin-Port unter `GET /metrics` (Bearer). Enthalten sind unter anderem `mcon_messages_accepted_total`, `mcon_provider_results_total`, `mcon_spam_decisions_total`, `mcon_webhook_results_total`, `mcon_messages_current` und `mcon_queue_depth`.

`/healthz` ist der Prozesscheck, `/readyz` prüft SQLite.

## Signale

`SIGTERM` und Interrupt beenden die Listener mit `server.shutdown_timeout`. `SIGHUP` lädt beide Dateien neu. Eine ungültige Datei lässt die laufende Fassung unangetastet. Adress- oder TLS-Listener-Änderungen brauchen einen Neustart.

Unter Windows gibt es kein SIGHUP; dort gilt `POST /admin/v1/reload`.

## Ollama

Auf ppclmt2 läuft Ollama lokal. `spam.ollama.base_url` zeigt auf `http://127.0.0.1:11434`. OpenRouter ist nur der Failover und braucht den API-Key in der Secrets-Datei.

## Backup und Archiv

Ist `backup.enabled` gesetzt, erzeugt der Dienst täglich `VACUUM INTO` unter `/var/lib/mcon/backups`. Die RFC822-Dateien liegen unter `/var/lib/mcon/archive/YYYY/MM/<id>.eml`.

## Smoke

Gegen einen laufenden Admin-Port: `scripts/smoke.sh http://127.0.0.1:8688`.
