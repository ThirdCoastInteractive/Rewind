-- +goose Up
ALTER TABLE videos ADD COLUMN IF NOT EXISTS links_harvested_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS videos_links_harvested_at_idx
    ON videos (links_harvested_at)
    WHERE channel_row_id IS NOT NULL AND links_harvested_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS videos_links_harvested_at_idx;
ALTER TABLE videos DROP COLUMN IF EXISTS links_harvested_at;
