CREATE TABLE IF NOT EXISTS messages (
    id              BIGSERIAL PRIMARY KEY,
    message_id      VARCHAR(64) NOT NULL UNIQUE,
    conversation_id VARCHAR(64) NOT NULL,
    turn            BIGINT      NOT NULL DEFAULT 0,
    role            VARCHAR(16) NOT NULL,
    content         JSONB       NOT NULL DEFAULT '[]',
    model           VARCHAR(64) NOT NULL DEFAULT '',
    metadata        JSONB       NOT NULL DEFAULT '{}',
    created_at      BIGINT      NOT NULL DEFAULT 0
);

--bun:split

CREATE INDEX IF NOT EXISTS idx_messages_conversation_turn ON messages (conversation_id, turn);
