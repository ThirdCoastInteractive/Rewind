-- +goose Up

-- Serial GPU work queue. UNIQUE (video_id, kind, transcript_hash) means a
-- second EnqueueMLJob with the same tuple is ON CONFLICT DO NOTHING.
CREATE TABLE ml_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    video_id UUID NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued',
    priority INT NOT NULL DEFAULT 100,
    transcript_hash TEXT NOT NULL DEFAULT '',
    model_digest TEXT NOT NULL DEFAULT '',
    prompt_version TEXT NOT NULL DEFAULT '',
    attempts INT NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    locked_at TIMESTAMPTZ,
    locked_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ml_jobs_kind_check CHECK (kind IN ('transcribe', 'refine_boundaries', 'context_windows')),
    CONSTRAINT ml_jobs_status_check CHECK (status IN ('queued', 'processing', 'waiting_model', 'succeeded', 'failed')),
    UNIQUE (video_id, kind, transcript_hash)
);
-- transcribe priority 10, refine_boundaries 50, context_windows 200 (lower runs first)
CREATE INDEX ml_jobs_claim_idx ON ml_jobs (priority, created_at)
    WHERE status IN ('queued', 'waiting_model');
CREATE INDEX ml_jobs_video_idx ON ml_jobs (video_id, kind);
-- At most one processing row: ClaimMLJob's NOT EXISTS plus this unique index
-- make serial GPU exclusive even if two workers race.
CREATE UNIQUE INDEX ml_jobs_serial_processing_uidx ON ml_jobs ((TRUE))
    WHERE status = 'processing';

CREATE TABLE context_window_sets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    video_id UUID NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    transcript_hash TEXT NOT NULL,
    model_digest TEXT NOT NULL,
    prompt_version TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued',
    metrics JSONB NOT NULL DEFAULT '{}'::JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT context_window_sets_status_check CHECK (status IN ('queued', 'processing', 'succeeded', 'failed', 'waiting_model')),
    UNIQUE (video_id, transcript_hash, model_digest, prompt_version)
);
CREATE INDEX context_window_sets_video_idx ON context_window_sets (video_id, created_at DESC);

ALTER TABLE context_windows
    ADD COLUMN set_id UUID REFERENCES context_window_sets(id) ON DELETE SET NULL,
    ADD COLUMN ordinal INT NOT NULL DEFAULT 0,
    ADD COLUMN cue_start INT NOT NULL DEFAULT 0,
    ADD COLUMN cue_end INT NOT NULL DEFAULT 0,
    ADD COLUMN generated_start_ts DOUBLE PRECISION,
    ADD COLUMN generated_end_ts DOUBLE PRECISION,
    ADD COLUMN confidence DOUBLE PRECISION,
    ADD COLUMN override_title BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN override_summary BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN override_bounds BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN stale BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX context_windows_set_idx ON context_windows (set_id);
CREATE INDEX context_windows_stale_idx ON context_windows (video_id, stale);

ALTER TABLE context_windows DROP CONSTRAINT IF EXISTS context_windows_origin_check;
ALTER TABLE context_windows ADD CONSTRAINT context_windows_origin_check
    CHECK (origin IN ('user', 'mcp', 'generated'));

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION notify_ml_jobs()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('ml_jobs', NEW.id::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER ml_jobs_notify_trigger
    AFTER INSERT ON ml_jobs
    FOR EACH ROW
    EXECUTE FUNCTION notify_ml_jobs();

-- +goose Down
DROP TRIGGER IF EXISTS ml_jobs_notify_trigger ON ml_jobs;
DROP FUNCTION IF EXISTS notify_ml_jobs();

UPDATE context_windows SET origin = 'user' WHERE origin = 'generated';
ALTER TABLE context_windows DROP CONSTRAINT IF EXISTS context_windows_origin_check;
ALTER TABLE context_windows ADD CONSTRAINT context_windows_origin_check
    CHECK (origin IN ('user', 'mcp'));

DROP INDEX IF EXISTS context_windows_stale_idx;
DROP INDEX IF EXISTS context_windows_set_idx;

ALTER TABLE context_windows
    DROP COLUMN IF EXISTS stale,
    DROP COLUMN IF EXISTS override_bounds,
    DROP COLUMN IF EXISTS override_summary,
    DROP COLUMN IF EXISTS override_title,
    DROP COLUMN IF EXISTS confidence,
    DROP COLUMN IF EXISTS generated_end_ts,
    DROP COLUMN IF EXISTS generated_start_ts,
    DROP COLUMN IF EXISTS cue_end,
    DROP COLUMN IF EXISTS cue_start,
    DROP COLUMN IF EXISTS ordinal,
    DROP COLUMN IF EXISTS set_id;

DROP TABLE IF EXISTS context_window_sets;
DROP INDEX IF EXISTS ml_jobs_serial_processing_uidx;
DROP INDEX IF EXISTS ml_jobs_video_idx;
DROP INDEX IF EXISTS ml_jobs_claim_idx;
DROP TABLE IF EXISTS ml_jobs;
