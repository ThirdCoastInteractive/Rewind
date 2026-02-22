-- +goose Up
CREATE TABLE video_transcripts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    video_id UUID NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    lang language_tag NOT NULL,
    format TEXT NOT NULL,
    text TEXT NOT NULL,
    raw TEXT NOT NULL,
    search TSVECTOR NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX video_transcripts_video_lang_uidx ON video_transcripts(video_id, lang);
CREATE INDEX video_transcripts_search_gin ON video_transcripts USING GIN(search);

-- +goose Down
DROP TABLE IF EXISTS video_transcripts;
