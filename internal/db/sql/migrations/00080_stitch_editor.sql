-- +goose Up
ALTER TABLE stitch_projects
    ADD COLUMN IF NOT EXISTS document JSONB,
    ADD COLUMN IF NOT EXISTS legacy_snapshot JSONB,
    ADD COLUMN IF NOT EXISTS document_version INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS revision BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS editor_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS undo_stack UUID[] NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS redo_stack UUID[] NOT NULL DEFAULT '{}';

CREATE TABLE IF NOT EXISTS stitch_edits (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES stitch_projects(id) ON DELETE CASCADE,
    owner_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    operation_key TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    revision BIGINT NOT NULL,
    actor_kind TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    actor_name TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL DEFAULT 'edit',
    before_document JSONB NOT NULL,
    after_document JSONB NOT NULL,
    changed_ids JSONB NOT NULL DEFAULT '[]',
    operations JSONB NOT NULL DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, operation_key),
    UNIQUE (project_id, revision)
);
CREATE INDEX IF NOT EXISTS stitch_edits_project_revision_idx
    ON stitch_edits (project_id, revision DESC);

ALTER TABLE stitch_jobs
    ADD COLUMN IF NOT EXISTS document_snapshot JSONB,
    ADD COLUMN IF NOT EXISTS project_revision BIGINT;

CREATE TABLE IF NOT EXISTS stitch_assets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES stitch_projects(id) ON DELETE CASCADE,
    owner_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    hash TEXT NOT NULL,
    path TEXT NOT NULL,
    mime TEXT NOT NULL DEFAULT '',
    width INTEGER NOT NULL DEFAULT 0,
    height INTEGER NOT NULL DEFAULT 0,
    size BIGINT NOT NULL DEFAULT 0,
    UNIQUE (project_id, hash)
);

-- +goose Down
DROP TABLE IF EXISTS stitch_assets;
ALTER TABLE stitch_jobs DROP COLUMN IF EXISTS project_revision, DROP COLUMN IF EXISTS document_snapshot;
DROP INDEX IF EXISTS stitch_edits_project_revision_idx;
DROP TABLE IF EXISTS stitch_edits;
ALTER TABLE stitch_projects DROP COLUMN IF EXISTS redo_stack, DROP COLUMN IF EXISTS undo_stack,
    DROP COLUMN IF EXISTS editor_enabled, DROP COLUMN IF EXISTS revision,
    DROP COLUMN IF EXISTS document_version, DROP COLUMN IF EXISTS legacy_snapshot, DROP COLUMN IF EXISTS document;
