CREATE TABLE IF NOT EXISTS messages (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id      VARCHAR(128) NOT NULL UNIQUE,
    conversation_id VARCHAR(64) NOT NULL,
    turn            BIGINT      NOT NULL DEFAULT 0,
    role            VARCHAR(16) NOT NULL,
    content         TEXT        NOT NULL DEFAULT '[]',
    model           VARCHAR(64) NOT NULL DEFAULT '',
    metadata        TEXT        NOT NULL DEFAULT '{}',
    created_at      BIGINT      NOT NULL DEFAULT 0
);

--bun:split

CREATE INDEX IF NOT EXISTS idx_messages_conversation_turn ON messages (conversation_id, turn);
