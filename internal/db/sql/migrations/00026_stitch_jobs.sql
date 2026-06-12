-- +goose Up
CREATE TABLE stitch_jobs (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_by       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title            TEXT NOT NULL DEFAULT '',
    format           TEXT NOT NULL DEFAULT 'mp4',
    quality          TEXT NOT NULL DEFAULT 'high',
    segments         JSONB NOT NULL DEFAULT '[]',
    global_filters   JSONB NOT NULL DEFAULT '[]',
    status           export_status NOT NULL DEFAULT 'queued',
    progress_pct     INTEGER NOT NULL DEFAULT 0,
    file_path        TEXT NOT NULL DEFAULT '',
    size_bytes       BIGINT NOT NULL DEFAULT 0,
    attempts         INTEGER NOT NULL DEFAULT 0,
    locked_at        TIMESTAMPTZ,
    locked_by        TEXT,
    pid              INTEGER,
    started_at       TIMESTAMPTZ,
    finished_at      TIMESTAMPTZ,
    last_error       TEXT,
    last_accessed_at TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_stitch_jobs_queue ON stitch_jobs (status, created_at)
    WHERE status IN ('queued', 'processing');

-- +goose Down
DROP INDEX IF EXISTS idx_stitch_jobs_queue;
DROP TABLE IF EXISTS stitch_jobs;
