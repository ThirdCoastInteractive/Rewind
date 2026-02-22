-- +goose Up
CREATE TABLE video_revisions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    video_id UUID NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    kind TEXT NOT NULL DEFAULT 'refresh',
    diff JSONB NOT NULL DEFAULT '{}',
    old_title TEXT,
    new_title TEXT,
    old_description TEXT,
    new_description TEXT,
    old_info JSONB,
    new_info JSONB
);

CREATE INDEX video_revisions_video_id_created_at_idx ON video_revisions(video_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS video_revisions;
