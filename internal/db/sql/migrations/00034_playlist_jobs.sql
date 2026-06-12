-- +goose Up
-- Playlist/channel archival: a "playlist" download job fans out into N child
-- "video" jobs (one per contained video) which flow through the normal
-- download -> ingest pipeline. These columns model that parent/child batch
-- relationship without changing any existing behaviour (kind defaults to
-- 'video', so all existing rows and single-URL submissions are unaffected).
ALTER TABLE download_jobs ADD COLUMN kind TEXT NOT NULL DEFAULT 'video';
ALTER TABLE download_jobs ADD COLUMN parent_job_id UUID REFERENCES download_jobs(id) ON DELETE SET NULL;
ALTER TABLE download_jobs ADD COLUMN batch_label TEXT;
ALTER TABLE download_jobs ADD COLUMN batch_total INT;

CREATE INDEX download_jobs_parent_idx ON download_jobs(parent_job_id) WHERE parent_job_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS download_jobs_parent_idx;
ALTER TABLE download_jobs DROP COLUMN IF EXISTS batch_total;
ALTER TABLE download_jobs DROP COLUMN IF EXISTS batch_label;
ALTER TABLE download_jobs DROP COLUMN IF EXISTS parent_job_id;
ALTER TABLE download_jobs DROP COLUMN IF EXISTS kind;
