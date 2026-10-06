CREATE TABLE IF NOT EXISTS conversations (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    conversation_id VARCHAR(64) NOT NULL UNIQUE,
    user_id         BIGINT      NOT NULL,
    title           VARCHAR(64) NOT NULL DEFAULT '',
    metadata        TEXT        DEFAULT '{}',
    created_at      BIGINT      NOT NULL
);
