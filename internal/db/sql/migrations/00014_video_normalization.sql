-- +goose Up
-- This migration originally backfilled video metadata from info JSON.
-- In the consolidated version, columns are created with the videos table.
-- This is kept as a placeholder for the backfill logic if needed on existing data.

-- Backfill search vectors (no-op for fresh installs, useful for existing data)
UPDATE videos SET search = to_tsvector('english', coalesce(title, '') || ' ' || coalesce(description, '') || ' ' || coalesce(uploader, ''))
WHERE search = '';

-- +goose Down
-- Nothing to undo - backfill only
