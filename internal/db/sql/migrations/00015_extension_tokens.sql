-- +goose Up
CREATE TABLE extension_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX idx_extension_tokens_user_id ON extension_tokens(user_id);
CREATE INDEX idx_extension_tokens_token ON extension_tokens(token) WHERE NOT revoked;
CREATE INDEX idx_extension_tokens_expires_at ON extension_tokens(expires_at);

-- +goose Down
DROP TABLE IF EXISTS extension_tokens;
