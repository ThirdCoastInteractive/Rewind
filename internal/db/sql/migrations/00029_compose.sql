-- +goose Up

-- Compose projects: saved editing state for multi-crop compositions
CREATE TABLE compose_projects (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    video_id UUID NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    created_by UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title TEXT NOT NULL DEFAULT 'Untitled Compose',
    canvas JSONB NOT NULL DEFAULT '{"width": 1080, "height": 1920, "color": "#000000"}',
    timeline JSONB NOT NULL DEFAULT '[]',
    format TEXT NOT NULL DEFAULT 'mp4',
    quality TEXT NOT NULL DEFAULT 'high',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_compose_projects_video_id ON compose_projects(video_id);
CREATE INDEX idx_compose_projects_created_by ON compose_projects(created_by);

-- Compose jobs: export queue for multi-crop compositions
CREATE TABLE compose_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID REFERENCES compose_projects(id) ON DELETE SET NULL,
    video_id UUID NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    created_by UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    canvas JSONB NOT NULL DEFAULT '{"width": 1080, "height": 1920, "color": "#000000"}',
    timeline JSONB NOT NULL DEFAULT '[]',
    format TEXT NOT NULL DEFAULT 'mp4',
    quality TEXT NOT NULL DEFAULT 'high',
    status export_status NOT NULL DEFAULT 'queued',
    progress_pct INT NOT NULL DEFAULT 0,
    pid INT,
    file_path TEXT NOT NULL DEFAULT '',
    size_bytes BIGINT NOT NULL DEFAULT 0,
    last_error TEXT,
    attempts INT NOT NULL DEFAULT 0,
    locked_at TIMESTAMPTZ,
    locked_by TEXT,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    last_accessed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_compose_jobs_status ON compose_jobs(status);
CREATE INDEX idx_compose_jobs_project_id ON compose_jobs(project_id);
CREATE INDEX idx_compose_jobs_video_id ON compose_jobs(video_id);

-- +goose Down

DROP TABLE IF EXISTS compose_jobs;
DROP TABLE IF EXISTS compose_projects;
