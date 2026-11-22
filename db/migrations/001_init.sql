CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS messages (
    id TEXT PRIMARY KEY,
    client_key TEXT NOT NULL,
    state TEXT NOT NULL,
    provider TEXT NOT NULL,
    from_addr TEXT NOT NULL,
    reply_to TEXT NOT NULL DEFAULT '',
    subject TEXT NOT NULL,
    text_body TEXT NOT NULL DEFAULT '',
    html_body TEXT NOT NULL DEFAULT '',
    headers_json TEXT NOT NULL DEFAULT '{}',
    tags_json TEXT NOT NULL DEFAULT '[]',
    metadata_json TEXT NOT NULL DEFAULT '{}',
    spam_hint TEXT NOT NULL DEFAULT '',
    content_sha256 TEXT NOT NULL,
    body_sha256 TEXT NOT NULL,
    rfc822_path TEXT NOT NULL DEFAULT '',
    rfc_message_id TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL,
    lease_until TEXT,
    leased_by TEXT NOT NULL DEFAULT '',
    next_attempt_at TEXT,
    last_error TEXT NOT NULL DEFAULT '',
    provider_message_id TEXT NOT NULL DEFAULT '',
    spam_score REAL,
    spam_reason TEXT NOT NULL DEFAULT '',
    spam_verdict TEXT NOT NULL DEFAULT '',
    spam_engine TEXT NOT NULL DEFAULT '',
    resend_of TEXT NOT NULL DEFAULT '',
    skip_spam INTEGER NOT NULL DEFAULT 0,
    idempotency_key TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    sent_at TEXT
);

CREATE TABLE IF NOT EXISTS recipients (
    message_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    address TEXT NOT NULL,
    position INTEGER NOT NULL,
    PRIMARY KEY (message_id, kind, position),
    FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS attachments (
    message_id TEXT NOT NULL,
    position INTEGER NOT NULL,
    filename TEXT NOT NULL,
    content_type TEXT NOT NULL,
    content_sha256 TEXT NOT NULL,
    size_bytes INTEGER NOT NULL,
    data BLOB NOT NULL,
    PRIMARY KEY (message_id, position),
    FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS idempotency (
    client_key TEXT NOT NULL,
    idem_key TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    message_id TEXT NOT NULL,
    response_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    PRIMARY KEY (client_key, idem_key)
);

CREATE TABLE IF NOT EXISTS nonces (
    client_key TEXT NOT NULL,
    nonce TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    PRIMARY KEY (client_key, nonce)
);

CREATE TABLE IF NOT EXISTS webhooks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id TEXT NOT NULL,
    client_key TEXT NOT NULL,
    event TEXT NOT NULL,
    payload TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 8,
    next_attempt_at TEXT,
    state TEXT NOT NULL,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS suppressions (
    address TEXT PRIMARY KEY,
    reason TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_messages_claim ON messages(state, next_attempt_at, created_at);
CREATE INDEX IF NOT EXISTS idx_messages_dedupe ON messages(client_key, body_sha256, created_at);
CREATE INDEX IF NOT EXISTS idx_nonces_exp ON nonces(expires_at);
CREATE INDEX IF NOT EXISTS idx_idem_exp ON idempotency(expires_at);
CREATE INDEX IF NOT EXISTS idx_webhooks_claim ON webhooks(state, next_attempt_at);
