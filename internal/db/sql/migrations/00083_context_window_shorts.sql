-- +goose Up

ALTER TABLE context_windows
    ADD COLUMN kind TEXT NOT NULL DEFAULT 'window',
    ADD COLUMN parent_id UUID REFERENCES context_windows(id) ON DELETE CASCADE,
    ADD COLUMN hook TEXT NOT NULL DEFAULT '';

ALTER TABLE context_windows
    ADD CONSTRAINT context_windows_kind_check CHECK (kind IN ('window', 'short'));

ALTER TABLE context_windows
    ADD CONSTRAINT context_windows_parent_kind_check CHECK (
        (kind = 'window' AND parent_id IS NULL) OR
        (kind = 'short' AND parent_id IS NOT NULL)
    );

CREATE INDEX context_windows_parent_idx ON context_windows (parent_id) WHERE parent_id IS NOT NULL;
CREATE INDEX context_windows_kind_idx ON context_windows (video_id, kind, stale);

-- +goose Down

DROP INDEX IF EXISTS context_windows_kind_idx;
DROP INDEX IF EXISTS context_windows_parent_idx;
ALTER TABLE context_windows DROP CONSTRAINT IF EXISTS context_windows_parent_kind_check;
ALTER TABLE context_windows DROP CONSTRAINT IF EXISTS context_windows_kind_check;
ALTER TABLE context_windows DROP COLUMN IF EXISTS hook;
ALTER TABLE context_windows DROP COLUMN IF EXISTS parent_id;
ALTER TABLE context_windows DROP COLUMN IF EXISTS kind;
