-- +goose Up
ALTER TABLE users ADD COLUMN sessions_invalidated_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE users DROP COLUMN IF EXISTS sessions_invalidated_at;
