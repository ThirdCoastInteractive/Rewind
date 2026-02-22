-- +goose NO TRANSACTION
-- +goose Up
-- Extend export_status enum to support queued state
-- ADD VALUE cannot run inside a transaction, so we use NO TRANSACTION mode
ALTER TYPE export_status ADD VALUE IF NOT EXISTS 'queued' BEFORE 'processing';

-- Add job lifecycle and progress columns to clip_exports
ALTER TABLE clip_exports
    ADD COLUMN IF NOT EXISTS attempts INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS locked_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS locked_by TEXT,
    ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS finished_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS progress_pct INTEGER NOT NULL DEFAULT 0;

-- Index for efficient queue polling (queued exports ordered by creation)
CREATE INDEX IF NOT EXISTS idx_clip_exports_queue ON clip_exports(status, created_at)
    WHERE status IN ('queued', 'processing');

-- +goose Down
-- Remove queue columns
ALTER TABLE clip_exports
    DROP COLUMN IF EXISTS attempts,
    DROP COLUMN IF EXISTS locked_at,
    DROP COLUMN IF EXISTS locked_by,
    DROP COLUMN IF EXISTS started_at,
    DROP COLUMN IF EXISTS finished_at,
    DROP COLUMN IF EXISTS progress_pct;

DROP INDEX IF EXISTS idx_clip_exports_queue;

-- Note: Cannot remove enum value in PostgreSQL, leave 'queued' in place
