-- +goose Up
-- Channel Watching: a user registers a channel/playlist URL plus a cron
-- schedule; the downloader periodically scans it for new videos and enqueues
-- them through the normal playlist -> child video job pipeline.

CREATE TABLE watched_channels (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    url              TEXT NOT NULL,
    label            TEXT NOT NULL DEFAULT '',
    cron_schedule    TEXT NOT NULL,
    enabled          BOOLEAN NOT NULL DEFAULT TRUE,
    -- backfill=TRUE: the first scan archives the channel's whole existing
    -- catalog. FALSE: the first scan only seeds the seen-ledger, so only
    -- videos published after the watch was added get archived.
    backfill         BOOLEAN NOT NULL DEFAULT FALSE,
    first_scan_done  BOOLEAN NOT NULL DEFAULT FALSE,
    next_scan_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_scan_at     TIMESTAMPTZ,
    last_scan_status TEXT,
    last_scan_error  TEXT,
    last_scan_found  INTEGER NOT NULL DEFAULT 0,
    UNIQUE (created_by, url)
);

CREATE INDEX idx_watched_channels_due ON watched_channels (next_scan_at) WHERE enabled;

-- Seen-ledger: every entry ever observed on a watched channel, keyed by the
-- same deterministic video UUID ingest derives (videoid.VideoUUID). Dedup
-- against this table (not just videos) means a failing or still-downloading
-- video is never re-enqueued by the next scan tick, and "only new from now on"
-- watches can mark the existing catalog seen without downloading it.
CREATE TABLE watched_channel_videos (
    watch_id      UUID NOT NULL REFERENCES watched_channels(id) ON DELETE CASCADE,
    video_id      UUID NOT NULL,
    entry_id      TEXT NOT NULL,
    url           TEXT NOT NULL,
    title         TEXT NOT NULL DEFAULT '',
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    enqueued      BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (watch_id, video_id)
);

-- Scan runs are playlist-kind download_jobs tagged with their watch, so the
-- jobs UI shows scans (with yt-dlp logs and grouped children) and the
-- scheduler can skip a watch whose previous scan is still queued/processing.
ALTER TABLE download_jobs ADD COLUMN watch_id UUID REFERENCES watched_channels(id) ON DELETE SET NULL;
CREATE INDEX download_jobs_watch_idx ON download_jobs(watch_id) WHERE watch_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS download_jobs_watch_idx;
ALTER TABLE download_jobs DROP COLUMN IF EXISTS watch_id;
DROP TABLE IF EXISTS watched_channel_videos;
DROP TABLE IF EXISTS watched_channels;
