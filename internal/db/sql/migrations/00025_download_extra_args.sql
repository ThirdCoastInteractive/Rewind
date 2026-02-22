-- +goose Up
ALTER TABLE download_jobs ADD COLUMN extra_args TEXT[] NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE download_jobs DROP COLUMN extra_args;
