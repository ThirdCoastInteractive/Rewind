-- +goose Up
ALTER TABLE videos ADD COLUMN probe_data JSONB;

-- +goose Down
ALTER TABLE videos DROP COLUMN IF EXISTS probe_data;
