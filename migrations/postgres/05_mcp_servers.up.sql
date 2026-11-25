CREATE TABLE IF NOT EXISTS mcp_servers (
    id          BIGSERIAL PRIMARY KEY,
    name        VARCHAR(64) NOT NULL UNIQUE,
    display     JSONB DEFAULT '{}',
    description TEXT NOT NULL DEFAULT '',
    icon        VARCHAR(256) NOT NULL DEFAULT '',
    transport   VARCHAR(16) NOT NULL,
    command     TEXT NOT NULL DEFAULT '',
    args        JSONB DEFAULT '[]',
    env         JSONB DEFAULT '{}',
    url         TEXT NOT NULL DEFAULT '',
    headers     JSONB DEFAULT '{}',
    credentials JSONB DEFAULT '[]',
    created_at  BIGINT NOT NULL,
    updated_at  BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS mcp_user_servers (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL,
    server_id   BIGINT NOT NULL,
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    credentials TEXT NOT NULL DEFAULT '',
    created_at  BIGINT NOT NULL,
    updated_at  BIGINT NOT NULL,
    UNIQUE (user_id, server_id)
);
