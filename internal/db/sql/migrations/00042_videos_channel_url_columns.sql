-- +goose Up
-- Channel pages aggregate by uploader and need each video's channel/uploader
-- URL. Extracting them from the info JSONB at query time detoasts every
-- video's full yt-dlp metadata blob (~25KB avg) on every page load (~900ms
-- for the /channels listing). STORED generated columns compute once at write
-- time, backfill during this migration's table rewrite, and stay consistent
-- with any future info refresh without touching ingest code.
ALTER TABLE videos ADD COLUMN channel_url TEXT GENERATED ALWAYS AS (COALESCE(NULLIF(info->>'channel_url', ''), '')) STORED;
ALTER TABLE videos ADD COLUMN uploader_url TEXT GENERATED ALWAYS AS (COALESCE(NULLIF(info->>'uploader_url', ''), '')) STORED;

-- +goose Down
ALTER TABLE videos DROP COLUMN IF EXISTS uploader_url;
ALTER TABLE videos DROP COLUMN IF EXISTS channel_url;
