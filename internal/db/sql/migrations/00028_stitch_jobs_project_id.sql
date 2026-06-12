-- +goose Up
ALTER TABLE stitch_jobs
    ADD COLUMN project_id UUID REFERENCES stitch_projects(id) ON DELETE SET NULL;

CREATE INDEX idx_stitch_jobs_project ON stitch_jobs (project_id)
    WHERE project_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_stitch_jobs_project;
ALTER TABLE stitch_jobs DROP COLUMN IF EXISTS project_id;
