-- +goose Up
ALTER TABLE compose_projects ALTER COLUMN video_id DROP NOT NULL;
ALTER TABLE compose_jobs ALTER COLUMN video_id DROP NOT NULL;

-- +goose Down
-- Backfill NULLs before restoring NOT NULL (use a dummy UUID that won't exist)
-- In practice the DOWN should only be run on dev; production should not revert.
ALTER TABLE compose_projects ALTER COLUMN video_id SET NOT NULL;
ALTER TABLE compose_jobs ALTER COLUMN video_id SET NOT NULL;
