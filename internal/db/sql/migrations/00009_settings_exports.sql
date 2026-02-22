-- +goose Up
CREATE TABLE instance_settings (
    id INTEGER PRIMARY KEY,
    registration_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    clip_export_storage_limit_bytes BIGINT NOT NULL DEFAULT 0,
    admin_emails TEXT[],
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO instance_settings (id) VALUES (1);

CREATE TABLE clip_exports (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    clip_id UUID NOT NULL REFERENCES clips(id) ON DELETE CASCADE,
    created_by UUID NOT NULL,
    format TEXT NOT NULL DEFAULT 'mp4',
    file_path TEXT NOT NULL,
    size_bytes BIGINT NOT NULL DEFAULT 0,
    status export_status NOT NULL DEFAULT 'processing',
    last_error TEXT,
    variant export_variant NOT NULL DEFAULT 'full',
    clip_updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_accessed_at TIMESTAMPTZ
);

CREATE INDEX idx_clip_exports_clip_id ON clip_exports(clip_id);
CREATE INDEX idx_clip_exports_lru ON clip_exports(last_accessed_at, created_at);
CREATE INDEX idx_clip_exports_variant ON clip_exports(variant);

-- +goose Down
DROP TABLE IF EXISTS clip_exports;
DROP TABLE IF EXISTS instance_settings;
