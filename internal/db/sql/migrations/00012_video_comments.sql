-- +goose Up
CREATE TABLE video_comments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    video_id UUID NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    source TEXT NOT NULL,
    comment_id TEXT NOT NULL,
    parent_id TEXT,
    author TEXT,
    author_id TEXT,
    author_url TEXT,
    published_at TIMESTAMPTZ,
    like_count BIGINT,
    text TEXT,
    raw JSONB NOT NULL,
    search TSVECTOR NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX video_comments_video_source_comment_id_uidx ON video_comments(video_id, source, comment_id);
CREATE INDEX video_comments_video_id_idx ON video_comments(video_id);
CREATE INDEX video_comments_search_gin ON video_comments USING GIN(search);

-- +goose Down
DROP TABLE IF EXISTS video_comments;
