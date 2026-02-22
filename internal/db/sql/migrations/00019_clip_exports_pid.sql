-- +goose Up
-- Add PID tracking for ffmpeg processes so we can kill orphaned ones
ALTER TABLE clip_exports
    ADD COLUMN IF NOT EXISTS pid INTEGER;

-- Index for finding exports by worker (for cleanup on restart)
CREATE INDEX IF NOT EXISTS idx_clip_exports_locked_by ON clip_exports(locked_by)
    WHERE locked_by IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_clip_exports_locked_by;
ALTER TABLE clip_exports DROP COLUMN IF EXISTS pid;
