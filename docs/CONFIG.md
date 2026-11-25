# Konfiguration

`mcon.yaml` und `secrets.yaml` werden getrennt geladen. Beispiele liegen unter `configs/*.example`. Secrets werden nicht ins Repository geschrieben.

`mcon -config` und `mcon -secrets` sind Pflicht, ersatzweise `MCON_CONFIG` und `MCON_SECRETS`. Ohne beide Dateien endet der Prozess mit Status 1. Es gibt **keinen** automatischen Suchpfad im Arbeitsverzeichnis. Unter systemd lautet der Standardpfad **`/etc/mcon/secrets.yaml`** (siehe [INSTALL.md](INSTALL.md)). SIGHUP und `POST /admin/v1/reload` übernehmen eine neue Fassung nur, wenn `Validate` erfolgreich ist.

## Wichtige Regeln

- `server.admin_addr` muss Loopback sein. Liegt `server.public_addr` nicht auf Loopback, ist TLS Pflicht.
- SMTP mit `tls_mode: none` ist nur zu `127.0.0.1`, `::1` oder `localhost` gültig. Fernstrecke nutzt `starttls` oder `tls`.
- Interne/abgelaufene SMTP-Zertifikate: `providers.items.<name>.tls_insecure_skip_verify: true` (nur SMTP; Standard `false`).
- Jeder Client braucht `allowed_from` und ein HMAC-Secret von mindestens 16 Zeichen. Das Admin-Token ebenfalls.
- `spam.mode` ist `failover`, `ollama` oder `openrouter`. `on_timeout` und `on_error` sind `send` oder `block`.
- OpenRouter braucht `secrets.spam.openrouter.api_key`, sobald der Modus es verwendet. Ollama spricht `spam.ollama.base_url` (auf ppclmt2 typisch `http://127.0.0.1:11434`).
- Provider-Namen und Client-IDs bestehen aus Buchstaben, Ziffern, Punkt, Unterstrich und Bindestrich.
- Ein Fallback verweist auf einen anderen definierten Provider und nie auf sich selbst.

## Clients

Der Schlüssel unter `clients:` (in den Beispielen `website`) ist nur ein **Platzhalter**. Der Name ist frei wählbar (z. B. `kmbcloud`, `intranet`) und ist zugleich die HMAC-`key_id` der absendenden Website. Derselbe Name muss in `secrets.yaml` unter `clients.<id>.hmac_secret` stehen.

`allowed_from` ist eine Liste **vollständiger E-Mail-Adressen** (mit `@`), keine Domains. Der Abgleich mit dem JSON-Feld `from` ist exakt (Groß/Klein egal). Domains allein (`kmbcloud.at`) sind ungültig und führen zu einem Validierungsfehler.

```yaml
clients:
  kmbcloud:
    allowed_from:
      - noreply@kmbcloud.at
      - kontakt@kmbcloud.at
  andere-site:
    allowed_from:
      - form@andere.at
```

Mehrere Websites: entweder mehrere Adressen unter einem Client (gemeinsames HMAC-Secret) oder je Website ein eigener Client mit eigenem Secret (empfohlen).

### `test_ui.client_key_id`

Wenn `test_ui.enabled: true` ist, **muss** `test_ui.client_key_id` genau auf einen existierenden Client-Namen zeigen. Nach dem Umbenennen von `website` also auch anpassen, sonst startet M-Con nicht (Validierungsfehler: `test_ui.client_key_id "…" is not a client`).

```yaml
test_ui:
  enabled: true
  client_key_id: kmbcloud   # gleicher Name wie unter clients:
```

Ist die Testseite nicht nötig: `test_ui.enabled: false` — dann entfällt die Pflicht für `client_key_id`.

## Pfade

| Schlüssel | Host-Pfad |
|---|---|
| `paths.data_dir` | `/var/lib/mcon` |
| `paths.db_file` | `/var/lib/mcon/mcon.db` |
| `paths.archive_dir` | `/var/lib/mcon/archive` |
| `paths.tmp_dir` | `/var/lib/mcon/tmp` |
| `paths.backup_dir` | `/var/lib/mcon/backups` |

Das State-Verzeichnis ist `0700`, die Secrets-Datei `0440` (`root:mcon`), die Konfiguration `0640` (`root:mcon`).

## Queue, Breaker, Dedupe

`queue.workers`, `poll_interval`, `lease`, `max_attempts` und der exponentielle Backoff (`backoff_initial`, `backoff_multiplier`, `backoff_max`) gelten pro Nachricht. `max_attempts` wird beim Annehmen auf die Zeile kopiert.

`circuit_breaker.failure_threshold` zählt transiente Fehler. Danach ist der Provider für `cooldown` offen; der nächste erlaubte Versuch ist half-open.

`dedupe.window` fasst gleiche `body_sha256` desselben Clients zusammen. Resends sind ausgenommen.

`backup.interval` schreibt per `VACUUM INTO` nach `paths.backup_dir`.

## Secrets

`secrets.yaml` enthält `admin.token`, `clients.<id>.hmac_secret`, optional `webhook_secret`, Provider-Zugangsdaten und `spam.openrouter.api_key`. Die Webhook-URL steht in `mcon.yaml` beim Client, nicht im Secret.
