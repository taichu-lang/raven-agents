CREATE TABLE IF NOT EXISTS skills (
    id          BIGSERIAL PRIMARY KEY,
    name        VARCHAR(64) NOT NULL,
    provider    VARCHAR(64) NOT NULL DEFAULT '',
    icon        VARCHAR(256) NOT NULL DEFAULT '',
    display     JSONB DEFAULT '{}',
    version     VARCHAR(32) NOT NULL DEFAULT '1.0.0',
    description TEXT NOT NULL DEFAULT '',
    skill_md    TEXT NOT NULL DEFAULT '',
    source      VARCHAR(64) NOT NULL DEFAULT '',
    metadata    JSONB DEFAULT '{}',
    created_at  BIGINT NOT NULL,
    UNIQUE (name, version)
);
