-- +goose Up
CREATE TABLE IF NOT EXISTS stitch_alignment_jobs (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 project_id UUID NOT NULL REFERENCES stitch_projects(id) ON DELETE CASCADE,
 owner_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 caption_id TEXT NOT NULL,
 language TEXT NOT NULL,
 model_version TEXT NOT NULL,
 source_video_id TEXT NOT NULL,
 source_start_us BIGINT NOT NULL,
 source_end_us BIGINT NOT NULL,
 start_us BIGINT NOT NULL,
 end_us BIGINT NOT NULL,
 text TEXT NOT NULL,
 alignment_key TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending',
 locked_by TEXT NOT NULL DEFAULT '',
 result JSONB NOT NULL DEFAULT '{}',
 error TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(project_id,alignment_key)
);
ALTER TABLE stitch_alignment_jobs ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();
CREATE INDEX IF NOT EXISTS stitch_alignment_jobs_owner_idx ON stitch_alignment_jobs(owner_id,project_id,status);
-- +goose Down
DROP TABLE IF EXISTS stitch_alignment_jobs;
