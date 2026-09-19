-- +goose Up
-- The uploader combobox filters by a case-insensitive prefix. The original
-- plain uploader index cannot support lower(uploader) LIKE 'prefix%'.
CREATE INDEX IF NOT EXISTS videos_uploader_lower_prefix_idx
ON videos (lower(uploader) text_pattern_ops)
WHERE media <> 'metadata' AND uploader <> '';

-- +goose Down
DROP INDEX IF EXISTS videos_uploader_lower_prefix_idx;
