# Installation

Vollständige Anleitung für einen systemd-Host (Linux). Zielpfade und Rechte entsprechen `deploy/mcon.service`, `deploy/mcon.sysusers` und `deploy/mcon.tmpfiles`.

## Arbeitsverzeichnis

Als Installateur zuerst in das Quellverzeichnis wechseln:

```bash
cd /usr/local/src
```

Dort liegt (oder wird geklont) das Repository `M-Con`. Alle nachfolgenden `git`-, `go build`- und `cp deploy/...`-Schritte beziehen sich darauf, sofern nicht anders angegeben.

## Kurzantwort: wo liegt `secrets.yaml`?

**Produktionspfad (systemd):**

```text
/etc/mcon/secrets.yaml
```

Die Unit startet M-Con so:

```text
/usr/local/bin/mcon -config /etc/mcon/mcon.yaml -secrets /etc/mcon/secrets.yaml
```

M-Con sucht `secrets.yaml` **nicht** automatisch im Arbeitsverzeichnis. Der Pfad muss immer gesetzt sein:

| Quelle | Wirkung |
|---|---|
| `-secrets /pfad/zur/secrets.yaml` | CLI-Flag (höchste Priorität, wenn gesetzt) |
| Umgebungsvariable `MCON_SECRETS` | Fallback, wenn `-secrets` leer ist |
| weder Flag noch Env | Prozess endet mit Status 1 |

Gleiches gilt für die Konfiguration: `-config` bzw. `MCON_CONFIG`.

**Rechte (Produktion):**

| Datei / Verzeichnis | Rechte | Owner |
|---|---|---|
| `/etc/mcon/` | `0750` | `root:mcon` |
| `/etc/mcon/mcon.yaml` | `0640` | `root:mcon` |
| `/etc/mcon/secrets.yaml` | `0440` | `root:mcon` |
| `/etc/mcon/tls/` | `0750` | `root:mcon` |
| `/var/lib/mcon/` (+ Unterordner) | `0700` | `mcon:mcon` |

`ProtectSystem=strict` und `ReadOnlyPaths=/etc/mcon` in der Unit erlauben dem Dienst nur Lesen unter `/etc/mcon`. Schreiben ist auf `/var/lib/mcon` beschränkt.

---

## Voraussetzungen

- Linux mit systemd
- Go **1.23+** auf der Build-Maschine (oder CI)
- Root-Rechte für Installation und Unit
- Ausgehender Netzzugang zum gewählten Mail-Provider (SMTP/API)
- Optional: lokales Ollama (z. B. auf ppclmt2 unter `http://127.0.0.1:11434`) für Spam-Checks
- Optional: TLS-Zertifikat/-Schlüssel für den Public-Listener, wenn die Public-Adresse nicht Loopback ist

---

## 1. Quellcode holen und bauen

```bash
sudo install -d -m 0755 /usr/local/src
cd /usr/local/src
sudo git clone https://github.com/WPreier/M-Con.git
cd M-Con
git checkout v0.1.4   # oder ein neuerer Tag

v=$(tr -d ' \r\n' < VERSION)
go build -ldflags "-X mcon/internal/version.Version=$v" -o mcon ./cmd/mcon
go build -ldflags "-X mcon/internal/version.Version=$v" -o mcon-ctl ./cmd/mcon-ctl

./mcon -version
./mcon-ctl -version
```

Beide Befehle müssen die gleiche SemVer ausgeben (z. B. `0.1.4`).

Optional Tests vor dem Deploy:

```bash
go test ./...
```

---

## 2. Systembenutzer und Verzeichnisse

```bash
# Benutzer und Verzeichnisse aus den Deploy-Vorlagen
sudo cp deploy/mcon.sysusers /usr/lib/sysusers.d/mcon.conf
sudo cp deploy/mcon.tmpfiles /usr/lib/tmpfiles.d/mcon.conf
sudo systemd-sysusers
sudo systemd-tmpfiles --create

# Falls sysusers/tmpfiles nicht verfügbar sind, manuell:
sudo useradd --system --home-dir /var/lib/mcon --shell /usr/sbin/nologin mcon 2>/dev/null || true
sudo install -d -o mcon -g mcon -m 0700 \
  /var/lib/mcon /var/lib/mcon/archive /var/lib/mcon/tmp /var/lib/mcon/backups
sudo install -d -o root -g mcon -m 0750 /etc/mcon /etc/mcon/tls
```

Entstehende Pfade:

```text
/etc/mcon/                 # Konfiguration + Secrets (nur lesen für den Dienst)
/etc/mcon/tls/             # optional: cert.pem, key.pem
/var/lib/mcon/             # SQLite, Archiv, Tmp, Backups
/var/lib/mcon/archive/
/var/lib/mcon/tmp/
/var/lib/mcon/backups/
```

---

## 3. Binaries und Unit installieren

```bash
sudo install -m 0755 mcon mcon-ctl /usr/local/bin/
sudo install -d -m 0755 /usr/local/share/doc/mcon
sudo install -m 0644 docs/*.md README.md CHANGELOG.md /usr/local/share/doc/mcon/
sudo cp deploy/mcon.service /etc/systemd/system/mcon.service
sudo systemctl daemon-reload
```

Die Unit erwartet fest:

- Binary: `/usr/local/bin/mcon`
- Config: `/etc/mcon/mcon.yaml`
- Secrets: `/etc/mcon/secrets.yaml`

Andere Pfade nur nach Anpassung von `ExecStart=` (oder Start ohne Unit mit Flags/Env).

---

## 4. Konfiguration anlegen

```bash
sudo cp configs/mcon.yaml.example /etc/mcon/mcon.yaml
sudo cp configs/secrets.yaml.example /etc/mcon/secrets.yaml
sudo chown root:mcon /etc/mcon/mcon.yaml /etc/mcon/secrets.yaml
sudo chmod 0640 /etc/mcon/mcon.yaml
sudo chmod 0440 /etc/mcon/secrets.yaml
```

Beide Dateien mit realen Werten befüllen. Details: [CONFIG.md](CONFIG.md).

### Pflicht in `mcon.yaml` (Auszug)

- `server.public_addr` / `server.admin_addr` (`admin_addr` muss Loopback sein)
- `paths.*` (Standard: unter `/var/lib/mcon`)
- mindestens einen Provider unter `providers.items`
- mindestens einen Client unter `clients` (Name beliebig, Beispiel `website` ist nur Platzhalter) mit `allowed_from` als vollständigen E-Mail-Adressen
- bei aktiver Testseite: `test_ui.client_key_id` auf denselben Client-Namen setzen (sonst Validierungsfehler)
- bei Public-Adresse außerhalb Loopback: `server.tls.enabled: true` plus Zertifikatspfad

### Pflicht in `secrets.yaml`

- `admin.token` (mindestens 16 Zeichen)
- `clients.<id>.hmac_secret` (mindestens 16 Zeichen) für jeden Client
- Provider-Zugangsdaten passend zum gewählten Provider-Typ
- optional `spam.openrouter.api_key`, wenn OpenRouter genutzt wird

**Niemals** echte Secrets ins Git-Repository legen. Nur die `*.example`-Dateien versionieren.

### TLS (Produktion hinter öffentlicher Adresse)

M-Con erzeugt keine Zertifikate. Bei Public-Adresse außerhalb Loopback sind PEM-Dateien Pflicht. Quellen:

1. **Let's Encrypt** (Certbot o. Ä.) — üblich für öffentliche Domains
2. **Firmen-CA / IT** — internes Zertifikat + Key als PEM
3. **Reverse-Proxy** (Nginx/Caddy) terminiert TLS; M-Con nur auf `127.0.0.1` → dann kein eigenes Zertifikat nötig

Beispiel Let's Encrypt:

```bash
sudo certbot certonly --standalone -d mcon.example.com
# typisch:
#   /etc/letsencrypt/live/mcon.example.com/fullchain.pem
#   /etc/letsencrypt/live/mcon.example.com/privkey.pem
```

**Dateien kopieren** (keine Symlinks nach `/etc/letsencrypt/live/...` — dort sind Verzeichnisse oft `0700 root`, dann scheitert User `mcon` mit `permission denied`):

```bash
sudo chown root:mcon /etc/mcon /etc/mcon/tls
sudo chmod 0750 /etc/mcon /etc/mcon/tls
sudo install -m 0640 -o root -g mcon \
  /etc/letsencrypt/live/mcon.example.com/fullchain.pem /etc/mcon/tls/cert.pem
sudo install -m 0640 -o root -g mcon \
  /etc/letsencrypt/live/mcon.example.com/privkey.pem  /etc/mcon/tls/key.pem
```

Lesbarkeit als Dienstbenutzer prüfen:

```bash
sudo -u mcon test -r /etc/mcon/tls/cert.pem && echo OK cert
sudo -u mcon test -r /etc/mcon/tls/key.pem && echo OK key
namei -l /etc/mcon/tls/cert.pem
```

In `mcon.yaml` auf diese Dateien zeigen (`server.tls.cert_file` / `key_file`). Nach Certbot-Erneuerung denselben Copy-Schritt (Deploy-Hook) wiederholen und `systemctl restart mcon` bzw. `kill -s HUP`.

---

## 5. Dienst starten und prüfen

```bash
sudo systemctl enable --now mcon
sudo systemctl status mcon --no-pager
journalctl -u mcon -e --no-pager
```

Smoke gegen den Admin-Port:

```bash
scripts/smoke.sh http://127.0.0.1:8688
# erwartet: healthz / readyz / version ohne Fehler
```

Lokaler Echtversand (SMTP, ohne systemd): siehe [TESTING.md](TESTING.md) und `scripts/local-mail-test.sh`.

Manuell:

```bash
curl -fsS http://127.0.0.1:8688/healthz
curl -fsS http://127.0.0.1:8688/readyz
curl -fsS http://127.0.0.1:8688/version
```

Metriken (Bearer = `admin.token` aus `secrets.yaml`):

```bash
curl -fsS -H "Authorization: Bearer <admin-token>" http://127.0.0.1:8688/metrics
```

Testseite (wenn `test_ui` aktiv): `http://127.0.0.1:8688/test/`

---

## 6. `mcon-ctl` einrichten

```bash
cp configs/mcon-ctl.yaml.example ~/mcon-ctl.yaml
chmod 0600 ~/mcon-ctl.yaml
# public_url, admin_url, key_id, hmac_secret, admin_token eintragen
```

Beispiele (Flags vor oder nach dem Unterbefehl; Config-Datei per `-config` oder Env `MCON_CTL_CONFIG`):

```bash
mcon-ctl -config ~/mcon-ctl.yaml health
mcon-ctl health -config ~/mcon-ctl.yaml
mcon-ctl -config ~/mcon-ctl.yaml version
mcon-ctl -config ~/mcon-ctl.yaml send --file testdata/contact.json
mcon-ctl -config ~/mcon-ctl.yaml status --id <message-id> --wait
```

Die ctl-Config darf **außerhalb** von `/etc/mcon` liegen (z. B. Home-Verzeichnis des Operators). Der Daemon liest sie nicht; er kennt nur `-config`/`-secrets` der Unit.

---

## 7. Firewall / Netz

| Port | Bindung (Default) | Zweck |
|---|---|---|
| `8687` | Public (`server.public_addr`) | HMAC Accept-API (von Webservern/Clients) |
| `8688` | `127.0.0.1` | Admin, Health, Metrics, Test-UI |

Admin-Port nicht öffentlich freigeben. Public-Port nur für bekannte Absender/Reverse-Proxy öffnen.

---

## 8. Reload und Updates

Konfiguration neu laden ohne Neustart (nur wenn Validate ok; Adress-/TLS-Wechsel brauchen Restart):

```bash
sudo systemctl kill -s HUP mcon
# oder:
curl -fsS -X POST -H "Authorization: Bearer <admin-token>" \
  http://127.0.0.1:8688/admin/v1/reload
```

Update auf neue Version:

```bash
cd /usr/local/src/M-Con
git fetch --tags
git checkout vX.Y.Z
v=$(tr -d ' \r\n' < VERSION)
go build -ldflags "-X mcon/internal/version.Version=$v" -o mcon ./cmd/mcon
go build -ldflags "-X mcon/internal/version.Version=$v" -o mcon-ctl ./cmd/mcon-ctl
sudo install -m 0755 mcon mcon-ctl /usr/local/bin/
sudo systemctl restart mcon
./mcon -version
```

---

## 9. Start ohne systemd (Debug / manuell)

```bash
export MCON_CONFIG=/etc/mcon/mcon.yaml
export MCON_SECRETS=/etc/mcon/secrets.yaml
/usr/local/bin/mcon
# oder explizit:
/usr/local/bin/mcon -config /etc/mcon/mcon.yaml -secrets /etc/mcon/secrets.yaml
```

Beliebige andere Pfade sind erlaubt, solange der Prozess die Dateien lesen kann. Unter der mitgelieferten Unit ist nur `/etc/mcon/secrets.yaml` verdrahtet.

---

## 10. Checkliste vor Go-Live

- [ ] `/etc/mcon/secrets.yaml` vorhanden, Rechte `0440`, Owner `root:mcon`
- [ ] alle `change-me-*` Werte ersetzt
- [ ] `admin.token` und HMAC-Secrets ≥ 16 Zeichen
- [ ] Provider-Zugangsdaten und Fallback getestet
- [ ] TLS aktiv, falls Public-Addr nicht Loopback
- [ ] `/etc/mcon/tls/cert.pem` und `key.pem` vorhanden, `0640 root:mcon`, **keine** Symlinks nach Let's Encrypt
- [ ] `sudo -u mcon test -r /etc/mcon/tls/cert.pem` erfolgreich
- [ ] `systemctl is-active mcon` → `active`
- [ ] `smoke.sh` bzw. `/healthz` + `/readyz` ok
- [ ] Testmail mit `mcon-ctl send` zugestellt
- [ ] Journal ohne wiederholte Provider-/Permission-Fehler

---

## 11. Typische Fehler

| Symptom | Ursache / Fix |
|---|---|
| `mcon: config and secrets are required` | `-config`/`-secrets` oder `MCON_CONFIG`/`MCON_SECRETS` fehlen |
| startet und stirbt sofort (Permission) | `secrets.yaml` nicht lesbar für User `mcon` → Gruppe `mcon`, Mode `0440` |
| `tls file .../cert.pem: permission denied` | User `mcon` darf Datei/Verzeichnis nicht lesen: `chown root:mcon`, `chmod 0750` auf `/etc/mcon` und `/etc/mcon/tls`, `chmod 0640` auf PEM; **keine** Symlinks nach `/etc/letsencrypt/live/` — Dateien kopieren, dann `sudo -u mcon test -r ...` |
| Unit startet, Config „nicht gefunden“ | Datei nicht unter `/etc/mcon/` oder falscher `ExecStart`-Pfad |
| Public ohne TLS abgelehnt | Public-Addr nicht Loopback → TLS Pflicht |
| Spam/OpenRouter-Fehler | `spam.openrouter.api_key` in `secrets.yaml` setzen oder Mode anpassen |
| `mcon-ctl`: `unknown command -config` | Binary älter als die Flag-vor-Befehl-Unterstützung; neu bauen/installieren, oder `mcon-ctl health -config ~/mcon-ctl.yaml` |

Weiterführend: [OPERATIONS.md](OPERATIONS.md), [CONFIG.md](CONFIG.md), [API.md](API.md), [TESTING.md](TESTING.md).
