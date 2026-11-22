# API

## Signatur

Jede öffentliche Anfrage trägt:

- `X-MCon-Key`
- `X-MCon-Timestamp` (Unix-Sekunden)
- `X-MCon-Nonce` (8–128 Zeichen, einmalig im Nonce-Fenster)
- `X-MCon-Signature` (hex HMAC-SHA256)

Kanonische Zeichenkette, Felder durch `\n` getrennt:

```
METHOD
PATH
TIMESTAMP
NONCE
hex(sha256(body))
```

`METHOD` wird großgeschrieben. `PATH` ist der URL-Pfad ohne Query. Ein GET signiert den SHA-256 der leeren Nutzlast (`e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`).

`POST /v1/messages` verlangt zusätzlich `Idempotency-Key` (1–200 Zeichen aus `A-Za-z0-9._:-`). Dieselbe Kombination aus Client, Key und Body-Hash liefert die erste Antwort mit HTTP 200. Ein anderer Body zum selben Key liefert 409.

## POST /v1/messages

```json
{
  "from": "noreply@example.com",
  "to": ["user@example.com"],
  "cc": [],
  "bcc": [],
  "reply_to": "person@example.com",
  "subject": "Kontakt",
  "text": "Hallo",
  "html": "<p>Hallo</p>",
  "attachments": [{"filename": "a.txt", "content_type": "text/plain", "content_base64": "aGVsbG8="}],
  "headers": {"X-Form": "contact"},
  "tags": ["contact"],
  "metadata": {"form": "contact"},
  "spam": {"hint": "contact form"},
  "provider": ""
}
```

`from` muss in `allowed_from` stehen. CR oder LF in Adressen, Betreff oder Headern ergibt 400. Bcc steht nur im Envelope, nicht im RFC822-Header.

Antwort 202:

```json
{"id": "01H...", "state": "received", "content_sha256": "..."}
```

Unterdrückte Empfänger werden mit 202 und `state: blocked` angenommen. Ein Treffer im Dedupe-Fenster setzt `deduped: true` und gibt die bestehende ID zurück.

## Weitere öffentliche Routen

- `GET /v1/messages/{id}` liefert Status nur für den signierenden Client.
- `POST /v1/messages/{id}/cancel` bricht ab, solange die Nachricht nicht geleast, gesendet oder tot ist.

## Admin

Bearer-Token: `Authorization: Bearer <admin.token>`. Ohne Token erreichbar sind nur `/healthz`, `/readyz` und `/version`. `/metrics` verlangt den Token.

- `GET /admin/v1/messages?state=&client=&limit=`
- `GET /admin/v1/messages/{id}`
- `GET /admin/v1/messages/{id}/eml`
- `POST /admin/v1/messages/{id}/resend` mit `{"skip_spam": false}`
- `POST /admin/v1/messages/{id}/replay`
- `POST /admin/v1/messages/{id}/cancel`
- `GET|POST /admin/v1/suppressions`, `DELETE /admin/v1/suppressions?address=`
- `POST /admin/v1/test/send` nimmt dasselbe JSON wie die öffentliche API an und signiert serverseitig mit `test_ui.client_key_id`
- `POST /admin/v1/reload` lädt beide Dateien neu, wenn sie gültig sind. Listen-Adressen bleiben bis zum Neustart.

## Webhooks

Ist `webhook_url` gesetzt, legt der Dienst Events `accepted`, `blocked`, `sent`, `dead` und `cancelled` in eine Outbox. Der POST trägt `X-MCon-Signature: sha256=<hex hmac des Bodys>` mit dem Webhook-Secret.

## mcon-ctl

```
mcon-ctl --config ctl.yaml send --file testdata/contact.json
mcon-ctl status --id <id> --wait --timeout 30s
mcon-ctl resend --id <id> --skip-spam
mcon-ctl replay --id <id>
mcon-ctl health
mcon-ctl version
```

Flags und Umgebungsvariablen (`MCON_PUBLIC_URL`, `MCON_ADMIN_URL`, `MCON_KEY_ID`, `MCON_HMAC_SECRET`, `MCON_ADMIN_TOKEN`) überschreiben die Datei.
