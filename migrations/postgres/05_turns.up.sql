CREATE TABLE IF NOT EXISTS turns (
    id               BIGSERIAL PRIMARY KEY,
    conversation_id  VARCHAR(64) NOT NULL,
    turn_seq         BIGINT      NOT NULL,
    user_message_id  VARCHAR(64) NOT NULL DEFAULT '',
    status           VARCHAR(32) NOT NULL DEFAULT 'running',
    lease_until      BIGINT      NOT NULL DEFAULT 0,
    epoch            BIGINT      NOT NULL DEFAULT 0,
    cancel_requested BOOLEAN     NOT NULL DEFAULT FALSE,
    iterations       BIGINT      NOT NULL DEFAULT 0,
    tokens_used      BIGINT      NOT NULL DEFAULT 0,
    last_message_id  VARCHAR(64) NOT NULL DEFAULT '',
    pending_tools    JSONB       NOT NULL DEFAULT '[]',
    created_at       BIGINT      NOT NULL DEFAULT 0,
    updated_at       BIGINT      NOT NULL DEFAULT 0
);

--bun:split

CREATE UNIQUE INDEX IF NOT EXISTS idx_turns_conversation_turn ON turns (conversation_id, turn_seq);
