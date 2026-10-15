CREATE TABLE IF NOT EXISTS users (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    name             VARCHAR(64) NOT NULL DEFAULT '',
    state            VARCHAR(16) NOT NULL DEFAULT 'ACTIVE',
    created_at       BIGINT      NOT NULL,
    last_auth_method VARCHAR(16) DEFAULT '',
    preferences      TEXT        DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS auth_credentials (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    method     VARCHAR(16) NOT NULL DEFAULT 'email',
    credential TEXT        NOT NULL,
    created_at BIGINT      NOT NULL
);
