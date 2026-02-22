-- +goose Up
CREATE TABLE cookies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    domain VARCHAR(255) NOT NULL,
    flag VARCHAR(10) NOT NULL,
    path VARCHAR(500) NOT NULL,
    secure VARCHAR(10) NOT NULL,
    expiration BIGINT NOT NULL,
    name VARCHAR(255) NOT NULL,
    value encrypted_string NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_cookies_user_id ON cookies(user_id);
CREATE UNIQUE INDEX idx_cookies_unique ON cookies(user_id, domain, name, path);

-- +goose Down
DROP TABLE IF EXISTS cookies;
