-- +goose Up
-- User-managed tags for organizing the (shared) video library. Tags are global,
-- matching the library: every video-listing query already returns all videos to
-- any user, so per-user tags would be surprising. created_by is provenance only.
CREATE TABLE tags (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL,
    slug       TEXT NOT NULL,            -- lowercased name, for case-insensitive uniqueness
    color      TEXT,                     -- optional accent, e.g. '#a67c52'
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by UUID
);
CREATE UNIQUE INDEX tags_slug_unique ON tags (slug);

CREATE TABLE video_tags (
    video_id   UUID NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    tag_id     UUID NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by UUID,
    PRIMARY KEY (video_id, tag_id)
);
CREATE INDEX video_tags_tag_idx ON video_tags (tag_id);

-- +goose Down
DROP TABLE IF EXISTS video_tags;
DROP TABLE IF EXISTS tags;
