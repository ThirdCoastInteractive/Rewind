-- +goose Up
CREATE TABLE api_tokens (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMPTZ,
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL DEFAULT '',
    token_hash   TEXT NOT NULL UNIQUE,
    scopes       TEXT[] NOT NULL DEFAULT ARRAY['mcp:read']::text[],
    revoked_at   TIMESTAMPTZ
);
CREATE INDEX api_tokens_user_id_idx ON api_tokens (user_id);

-- +goose Down
DROP TABLE IF EXISTS api_tokens;
