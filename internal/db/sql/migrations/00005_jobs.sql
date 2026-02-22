-- +goose Up
CREATE TABLE download_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    url TEXT NOT NULL,
    archived_by UUID NOT NULL,
    status job_status NOT NULL DEFAULT 'queued',
    attempts INT NOT NULL DEFAULT 0,
    last_error TEXT,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    spool_dir TEXT,
    info_json_path TEXT,
    video_id UUID REFERENCES videos(id),
    -- Options
    refresh BOOLEAN NOT NULL DEFAULT FALSE,
    -- Process tracking
    process_pid BIGINT,
    -- Soft delete
    archived BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX download_jobs_status_created_at_idx ON download_jobs(status, created_at);
CREATE INDEX download_jobs_archived_idx ON download_jobs(archived, created_at DESC);

CREATE TABLE ingest_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    download_job_id UUID NOT NULL REFERENCES download_jobs(id) ON DELETE CASCADE,
    status job_status NOT NULL DEFAULT 'queued',
    attempts INT NOT NULL DEFAULT 0,
    last_error TEXT,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ
);

CREATE INDEX ingest_jobs_status_created_at_idx ON ingest_jobs(status, created_at);

-- Notify functions for job queue polling
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION notify_download_jobs()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('download_jobs', NEW.id::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION notify_ingest_jobs()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('ingest_jobs', NEW.id::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER download_jobs_notify_trigger
    AFTER INSERT ON download_jobs
    FOR EACH ROW
    EXECUTE FUNCTION notify_download_jobs();

CREATE TRIGGER ingest_jobs_notify_trigger
    AFTER INSERT ON ingest_jobs
    FOR EACH ROW
    EXECUTE FUNCTION notify_ingest_jobs();

-- +goose Down
DROP TRIGGER IF EXISTS ingest_jobs_notify_trigger ON ingest_jobs;
DROP TRIGGER IF EXISTS download_jobs_notify_trigger ON download_jobs;
DROP FUNCTION IF EXISTS notify_ingest_jobs();
DROP FUNCTION IF EXISTS notify_download_jobs();
DROP TABLE IF EXISTS ingest_jobs;
DROP TABLE IF EXISTS download_jobs;
