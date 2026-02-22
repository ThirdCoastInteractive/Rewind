-- +goose Up
CREATE TABLE videos (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    src TEXT NOT NULL,
    archived_by UUID NOT NULL,
    title TEXT NOT NULL,
    info JSONB NOT NULL DEFAULT '{}',
    comments JSONB NOT NULL DEFAULT '[]',
    -- File paths
    video_path TEXT,
    thumbnail_path TEXT,
    -- Normalized metadata from info JSON
    description TEXT NOT NULL DEFAULT '',
    tags TEXT[] NOT NULL DEFAULT ARRAY[]::text[],
    uploader TEXT NOT NULL DEFAULT '',
    uploader_id TEXT,
    channel_id TEXT,
    upload_date DATE,
    duration_seconds INT,
    view_count BIGINT,
    like_count BIGINT,
    -- Thumbnail placeholder gradients
    thumb_gradient_start TEXT,
    thumb_gradient_end TEXT,
    thumb_gradient_angle INT,
    -- File deduplication
    file_hash TEXT,
    file_size BIGINT,
    -- Asset status tracking
    assets_status asset_status_map NOT NULL DEFAULT '{}',
    -- Full-text search
    search TSVECTOR NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX videos_src_unique ON videos(src);
CREATE INDEX videos_search_gin ON videos USING GIN(search);
CREATE INDEX videos_upload_date_idx ON videos(upload_date);
CREATE INDEX videos_uploader_idx ON videos(uploader);
CREATE INDEX idx_videos_file_hash ON videos(file_hash) WHERE file_hash IS NOT NULL;

CREATE TABLE clips (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    video_id UUID NOT NULL REFERENCES videos(id),
    start_ts DOUBLE PRECISION NOT NULL,
    end_ts DOUBLE PRECISION NOT NULL,
    duration DOUBLE PRECISION NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by UUID NOT NULL,
    -- Metadata
    title TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    color TEXT NOT NULL DEFAULT '#a67c52',
    tags JSONB NOT NULL DEFAULT '[]',
    -- Multi-crop system
    crops JSONB DEFAULT '[]'
);

CREATE INDEX idx_clips_crops ON clips USING GIN(crops);

-- +goose Down
DROP TABLE IF EXISTS clips;
DROP TABLE IF EXISTS videos;
