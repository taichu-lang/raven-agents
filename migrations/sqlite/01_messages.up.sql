CREATE TABLE IF NOT EXISTS messages (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id      VARCHAR(128) NOT NULL UNIQUE,
    conversation_id VARCHAR(64) NOT NULL,
    turn_seq        BIGINT      NOT NULL DEFAULT 0,
    role            VARCHAR(16) NOT NULL,
    content         TEXT        NOT NULL DEFAULT '[]',
    model           VARCHAR(64) NOT NULL DEFAULT '',
    metadata        TEXT        NOT NULL DEFAULT '{}',
    created_at      BIGINT      NOT NULL DEFAULT 0
);

--bun:split

CREATE INDEX IF NOT EXISTS idx_messages_conversation_turn ON messages (conversation_id, turn_seq);

--bun:split

CREATE TABLE IF NOT EXISTS message_summaries (
    id                      INTEGER PRIMARY KEY AUTOINCREMENT,
    conversation_id         VARCHAR(64)  NOT NULL,
    turn_seq                BIGINT       NOT NULL DEFAULT 0,
    covers_from_message_id  VARCHAR(128) NOT NULL,
    covers_to_message_id    VARCHAR(128) NOT NULL,
    summary                 TEXT         NOT NULL DEFAULT '',
    tokens                  BIGINT       NOT NULL DEFAULT 0,
    created_at              BIGINT       NOT NULL DEFAULT 0
);

--bun:split

CREATE INDEX IF NOT EXISTS idx_message_summaries_conversation ON message_summaries (conversation_id, id);
