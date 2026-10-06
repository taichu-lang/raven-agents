create table if not exists users (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(64) NOT NULL DEFAULT '',
    state VARCHAR(16) NOT NULL DEFAULT 'ACTIVE',
    created_at BIGINT NOT NULL,
    last_auth_method VARCHAR(16) DEFAULT '',
    preferences JSONB DEFAULT '{}'
);

create table if not exists auth_credentials (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES USERS(ID) ON DELETE CASCADE,
    method VARCHAR(16) NOT NULL DEFAULT 'email',
    credential JSONB NOT NULL,
    created_at BIGINT NOT NULL
)

