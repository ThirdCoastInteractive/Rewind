-- +goose Up

ALTER TABLE videos
    ADD COLUMN subtitle_state TEXT NOT NULL DEFAULT 'pending',
    ADD COLUMN subtitle_checked_at TIMESTAMPTZ,
    ADD COLUMN subtitle_last_error TEXT NOT NULL DEFAULT '',
    ADD COLUMN transcript_version BIGINT NOT NULL DEFAULT 0;

ALTER TABLE videos ADD CONSTRAINT videos_subtitle_state_check
    CHECK (subtitle_state IN ('pending', 'manual', 'automatic', 'unavailable', 'failed'));
CREATE INDEX videos_subtitle_pending_idx ON videos (subtitle_state, subtitle_checked_at)
    WHERE subtitle_state IN ('pending', 'failed');

CREATE TABLE catalog_crawls (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    channel_id UUID NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    requested_by UUID NOT NULL REFERENCES users(id),
    feed_url TEXT NOT NULL,
    feed_kind TEXT NOT NULL DEFAULT 'videos',
    platform TEXT NOT NULL,
    next_page_index INTEGER NOT NULL DEFAULT 1,
    page_size INTEGER NOT NULL DEFAULT 100,
    overlap_size INTEGER NOT NULL DEFAULT 10,
    status TEXT NOT NULL DEFAULT 'queued',
    attempts INTEGER NOT NULL DEFAULT 0,
    retry_at TIMESTAMPTZ,
    entries_seen BIGINT NOT NULL DEFAULT 0,
    entries_added BIGINT NOT NULL DEFAULT 0,
    entries_updated BIGINT NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    refresh BOOLEAN NOT NULL DEFAULT FALSE,
    locked_at TIMESTAMPTZ,
    locked_by TEXT NOT NULL DEFAULT '',
    finished_at TIMESTAMPTZ,
    CONSTRAINT catalog_crawls_feed_kind_check CHECK (feed_kind IN ('videos', 'shorts', 'streams')),
    CONSTRAINT catalog_crawls_status_check CHECK (status IN ('queued', 'running', 'paused', 'retry_wait', 'complete', 'cancelled', 'failed'))
);
CREATE INDEX catalog_crawls_claim_idx ON catalog_crawls (platform, status, retry_at, created_at);
CREATE UNIQUE INDEX catalog_crawls_active_feed_uidx ON catalog_crawls (channel_id, feed_kind)
    WHERE status IN ('queued', 'running', 'paused', 'retry_wait');

CREATE TABLE context_windows (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    video_id UUID NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    start_ts DOUBLE PRECISION NOT NULL,
    end_ts DOUBLE PRECISION NOT NULL,
    title TEXT NOT NULL,
    summary TEXT NOT NULL DEFAULT '',
    topics TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    entities TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    search TSVECTOR NOT NULL DEFAULT '',
    origin TEXT NOT NULL DEFAULT 'user',
    source_query TEXT NOT NULL DEFAULT '',
    transcript_cue_evidence JSONB NOT NULL DEFAULT '[]'::JSONB,
    transcript_version BIGINT NOT NULL DEFAULT 0,
    boundary_quality TEXT NOT NULL DEFAULT 'cue',
    created_by UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT context_windows_bounds_check CHECK (start_ts >= 0 AND end_ts > start_ts),
    CONSTRAINT context_windows_origin_check CHECK (origin IN ('user', 'mcp')),
    CONSTRAINT context_windows_boundary_check CHECK (boundary_quality IN ('cue', 'waveform', 'manual'))
);
CREATE INDEX context_windows_video_time_idx ON context_windows (video_id, start_ts, end_ts);
CREATE INDEX context_windows_search_gin ON context_windows USING GIN (search);

CREATE OR REPLACE FUNCTION context_window_search_vector(title TEXT, summary TEXT, topics TEXT[], entities TEXT[])
RETURNS TSVECTOR LANGUAGE SQL IMMUTABLE AS $$
    SELECT setweight(to_tsvector('simple', COALESCE(title, '')), 'A') ||
           setweight(to_tsvector('simple', COALESCE(summary, '')), 'B') ||
           setweight(to_tsvector('simple', COALESCE(array_to_string(topics, ' '), '')), 'B') ||
           setweight(to_tsvector('simple', COALESCE(array_to_string(entities, ' '), '')), 'B')
$$;

CREATE TABLE compilation_plans (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_by UUID NOT NULL REFERENCES users(id),
    creator_id UUID REFERENCES creators(id) ON DELETE SET NULL,
    source_query TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft',
    revision INTEGER NOT NULL DEFAULT 1,
    estimated_duration DOUBLE PRECISION NOT NULL DEFAULT 0,
    stitch_project_id UUID REFERENCES stitch_projects(id) ON DELETE SET NULL,
    stitch_job_id UUID REFERENCES stitch_jobs(id) ON DELETE SET NULL,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT compilation_plans_status_check CHECK (status IN ('draft', 'waiting_media', 'ready', 'rendering', 'complete', 'failed'))
);

CREATE TABLE compilation_plan_segments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id UUID NOT NULL REFERENCES compilation_plans(id) ON DELETE CASCADE,
    position INTEGER NOT NULL,
    video_id UUID NOT NULL REFERENCES videos(id),
    start_ts DOUBLE PRECISION NOT NULL,
    end_ts DOUBLE PRECISION NOT NULL,
    context_window_id UUID REFERENCES context_windows(id) ON DELETE SET NULL,
    match_evidence JSONB NOT NULL DEFAULT '{}'::JSONB,
    selection_rationale TEXT NOT NULL DEFAULT '',
    media_ready BOOLEAN NOT NULL DEFAULT FALSE,
    failure_state TEXT NOT NULL DEFAULT '',
    download_job_id UUID REFERENCES download_jobs(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT compilation_segment_bounds_check CHECK (start_ts >= 0 AND end_ts > start_ts),
    UNIQUE (plan_id, position)
);
CREATE INDEX compilation_plans_creator_idx ON compilation_plans (creator_id, updated_at DESC);
CREATE INDEX compilation_segments_plan_idx ON compilation_plan_segments (plan_id, position);

-- Transcript writes increment videos.transcript_version. Context-window rows keep
-- the version they were based on, making stale evidence a cheap comparison.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION bump_video_transcript_version() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    UPDATE videos SET transcript_version = transcript_version + 1, updated_at = NOW()
    WHERE id = NEW.video_id;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER video_transcripts_version_trigger
AFTER INSERT OR UPDATE OF text, cues ON video_transcripts
FOR EACH ROW EXECUTE FUNCTION bump_video_transcript_version();

-- +goose Down
DROP TRIGGER IF EXISTS video_transcripts_version_trigger ON video_transcripts;
DROP FUNCTION IF EXISTS bump_video_transcript_version();
DROP TABLE IF EXISTS compilation_plan_segments;
DROP TABLE IF EXISTS compilation_plans;
DROP FUNCTION IF EXISTS context_window_search_vector(TEXT, TEXT, TEXT[], TEXT[]);
DROP TABLE IF EXISTS context_windows;
DROP TABLE IF EXISTS catalog_crawls;
DROP INDEX IF EXISTS videos_subtitle_pending_idx;
ALTER TABLE videos DROP CONSTRAINT IF EXISTS videos_subtitle_state_check;
ALTER TABLE videos
    DROP COLUMN IF EXISTS transcript_version,
    DROP COLUMN IF EXISTS subtitle_last_error,
    DROP COLUMN IF EXISTS subtitle_checked_at,
    DROP COLUMN IF EXISTS subtitle_state;
