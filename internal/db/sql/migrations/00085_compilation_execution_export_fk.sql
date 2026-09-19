-- +goose Up
-- Compilation executions recorded a stitch export as a hard FK. Plans already
-- use ON DELETE SET NULL; executions should too, so deleting a rendered export
-- does not require application-level cleanup.
ALTER TABLE compilation_executions
    DROP CONSTRAINT IF EXISTS compilation_executions_stitch_job_id_fkey,
    ADD CONSTRAINT compilation_executions_stitch_job_id_fkey
        FOREIGN KEY (stitch_job_id) REFERENCES stitch_jobs(id) ON DELETE SET NULL;

ALTER TABLE compilation_executions
    DROP CONSTRAINT IF EXISTS compilation_executions_stitch_project_id_fkey,
    ADD CONSTRAINT compilation_executions_stitch_project_id_fkey
        FOREIGN KEY (stitch_project_id) REFERENCES stitch_projects(id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE compilation_executions
    DROP CONSTRAINT IF EXISTS compilation_executions_stitch_job_id_fkey,
    ADD CONSTRAINT compilation_executions_stitch_job_id_fkey
        FOREIGN KEY (stitch_job_id) REFERENCES stitch_jobs(id);

ALTER TABLE compilation_executions
    DROP CONSTRAINT IF EXISTS compilation_executions_stitch_project_id_fkey,
    ADD CONSTRAINT compilation_executions_stitch_project_id_fkey
        FOREIGN KEY (stitch_project_id) REFERENCES stitch_projects(id);
