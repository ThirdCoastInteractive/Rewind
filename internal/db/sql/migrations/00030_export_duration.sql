-- +goose Up
ALTER TABLE stitch_jobs ADD COLUMN duration_seconds FLOAT8;
ALTER TABLE compose_jobs ADD COLUMN duration_seconds FLOAT8;

-- +goose Down
ALTER TABLE stitch_jobs DROP COLUMN IF EXISTS duration_seconds;
ALTER TABLE compose_jobs DROP COLUMN IF EXISTS duration_seconds;
