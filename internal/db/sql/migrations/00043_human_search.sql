-- +goose Up
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Weighted tsvector used at insert and for the one-shot backfill. `simple`
-- keeps uploader names intact; prefix matching covers typeahead.
CREATE OR REPLACE FUNCTION video_search_vector(title text, uploader text, tags text[], description text)
RETURNS tsvector
LANGUAGE sql
IMMUTABLE
AS $$
  SELECT
    setweight(to_tsvector('simple', coalesce(title, '')), 'A') ||
    setweight(to_tsvector('simple', coalesce(uploader, '')), 'A') ||
    setweight(to_tsvector('simple', coalesce(array_to_string(tags, ' '), '')), 'B') ||
    setweight(to_tsvector('simple', coalesce(description, '')), 'C')
$$;

UPDATE videos
SET search = video_search_vector(title, uploader, tags, description);

CREATE INDEX IF NOT EXISTS videos_title_trgm ON videos USING gin (title gin_trgm_ops);
CREATE INDEX IF NOT EXISTS videos_uploader_trgm ON videos USING gin (uploader gin_trgm_ops);

ALTER TABLE video_transcripts
    ADD COLUMN IF NOT EXISTS cues JSONB NOT NULL DEFAULT '[]'::jsonb;

-- +goose Down
ALTER TABLE video_transcripts DROP COLUMN IF EXISTS cues;
DROP INDEX IF EXISTS videos_uploader_trgm;
DROP INDEX IF EXISTS videos_title_trgm;
DROP FUNCTION IF EXISTS video_search_vector(text, text, text[], text);
-- Keep pg_trgm; other objects may depend on it after this lands.
