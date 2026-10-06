CREATE TABLE IF NOT EXISTS conversations (
    id BIGSERIAL PRIMARY KEY,
    conversation_id VARCHAR(64) NOT NULL UNIQUE,
    user_id BIGINT NOT NULL,
    title VARCHAR(64) NOT NULL DEFAULT '',
    metadata JSONB DEFAULT '{}',
    created_at BIGINT NOT NULL
);