-- +goose Up
CREATE TABLE markers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    video_id UUID NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    timestamp DOUBLE PRECISION NOT NULL CHECK (timestamp >= 0),
    title TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    color TEXT NOT NULL DEFAULT '#a67c52',
    marker_type marker_type NOT NULL DEFAULT 'point',
    duration DOUBLE PRECISION,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by UUID NOT NULL
);

CREATE INDEX idx_markers_video_id ON markers(video_id);
CREATE INDEX idx_markers_timestamp ON markers(video_id, timestamp);
CREATE INDEX idx_markers_created_by ON markers(created_by);
CREATE INDEX idx_markers_duration ON markers(video_id, timestamp, duration) WHERE duration IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS markers;
