-- +goose Up
-- Allows specifying which asset(s) to regenerate.
-- NULL means "all assets" (backward compatible with existing regeneration jobs).
ALTER TABLE ingest_jobs ADD COLUMN asset_scope TEXT;

-- +goose Down
ALTER TABLE ingest_jobs DROP COLUMN IF EXISTS asset_scope;
