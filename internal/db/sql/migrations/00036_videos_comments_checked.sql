-- +goose Up
-- Tracks when the downloader last attempted to fetch comments for a video, so
-- the comment catch-up loop backfills older videos (downloaded before comment
-- ingest existed) without repeatedly re-fetching videos that genuinely have none.
ALTER TABLE videos ADD COLUMN comments_checked_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE videos DROP COLUMN IF EXISTS comments_checked_at;
