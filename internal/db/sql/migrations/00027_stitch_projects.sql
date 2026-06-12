-- +goose Up
CREATE TABLE stitch_projects (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_by      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title           TEXT NOT NULL DEFAULT 'Untitled',
    format          TEXT NOT NULL DEFAULT 'mp4',
    quality         TEXT NOT NULL DEFAULT 'high',
    segments        JSONB NOT NULL DEFAULT '[]',
    global_filters  JSONB NOT NULL DEFAULT '[]',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_stitch_projects_user ON stitch_projects (created_by, updated_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_stitch_projects_user;
DROP TABLE IF EXISTS stitch_projects;
