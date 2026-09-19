-- +goose Up
ALTER TABLE stitch_jobs ADD COLUMN IF NOT EXISTS export_operation_key TEXT;
ALTER TABLE stitch_jobs ADD COLUMN IF NOT EXISTS render_kind TEXT NOT NULL DEFAULT 'export';
ALTER TABLE stitch_jobs ADD COLUMN IF NOT EXISTS project_revision BIGINT;
ALTER TABLE stitch_jobs ADD COLUMN IF NOT EXISTS document_snapshot JSONB;
ALTER TABLE stitch_jobs ADD COLUMN IF NOT EXISTS range_start_us BIGINT;
ALTER TABLE stitch_jobs ADD COLUMN IF NOT EXISTS range_end_us BIGINT;
ALTER TABLE stitch_jobs ADD COLUMN IF NOT EXISTS frame_time_us BIGINT;
ALTER TABLE stitch_jobs ADD COLUMN IF NOT EXISTS render_options JSONB NOT NULL DEFAULT '{}';
ALTER TABLE stitch_jobs ADD COLUMN IF NOT EXISTS render_request_hash TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS stitch_jobs_project_operation_key_idx ON stitch_jobs(project_id, export_operation_key) WHERE project_id IS NOT NULL AND export_operation_key IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS stitch_jobs_project_operation_key_idx;
ALTER TABLE stitch_jobs DROP COLUMN IF EXISTS render_request_hash, DROP COLUMN IF EXISTS render_options, DROP COLUMN IF EXISTS frame_time_us, DROP COLUMN IF EXISTS range_end_us, DROP COLUMN IF EXISTS range_start_us, DROP COLUMN IF EXISTS render_kind, DROP COLUMN IF EXISTS export_operation_key;
