-- +goose Up

CREATE TABLE creators (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    name       TEXT NOT NULL,
    notes      TEXT NOT NULL DEFAULT '',
    search     TSVECTOR NOT NULL DEFAULT ''
);
CREATE INDEX creators_search_gin ON creators USING GIN (search);
CREATE INDEX creators_name_trgm ON creators USING GIN (name gin_trgm_ops);

CREATE TABLE channels (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    platform       TEXT NOT NULL,
    identity_key   TEXT NOT NULL,
    channel_id     TEXT NOT NULL DEFAULT '',
    uploader       TEXT NOT NULL DEFAULT '',
    canonical_url  TEXT NOT NULL DEFAULT '',
    creator_id     UUID REFERENCES creators(id) ON DELETE SET NULL,
    search         TSVECTOR NOT NULL DEFAULT '',
    UNIQUE (platform, identity_key)
);
CREATE INDEX channels_creator_id_idx ON channels (creator_id);
CREATE INDEX channels_search_gin ON channels USING GIN (search);
CREATE INDEX channels_uploader_trgm ON channels USING GIN (uploader gin_trgm_ops);

CREATE TABLE channel_edges (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    from_channel_id  UUID NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    to_channel_id    UUID REFERENCES channels(id) ON DELETE SET NULL,
    to_url           TEXT NOT NULL DEFAULT '',
    kind             TEXT NOT NULL,
    evidence         TEXT NOT NULL DEFAULT '',
    video_id         UUID REFERENCES videos(id) ON DELETE SET NULL,
    weight           INTEGER NOT NULL DEFAULT 1
);
CREATE UNIQUE INDEX channel_edges_identity_uidx
    ON channel_edges (from_channel_id, kind, COALESCE(to_channel_id::text, to_url));
CREATE INDEX channel_edges_to_channel_idx ON channel_edges (to_channel_id) WHERE to_channel_id IS NOT NULL;

ALTER TABLE videos ADD COLUMN IF NOT EXISTS channel_row_id UUID REFERENCES channels(id) ON DELETE SET NULL;
ALTER TABLE videos ADD COLUMN IF NOT EXISTS format TEXT NOT NULL DEFAULT 'video';
ALTER TABLE videos ADD COLUMN IF NOT EXISTS metadata_refreshed_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS videos_channel_row_id_idx ON videos (channel_row_id);
CREATE INDEX IF NOT EXISTS videos_format_idx ON videos (format);

ALTER TABLE markers ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'user';
ALTER TABLE markers ADD COLUMN IF NOT EXISTS source_ref TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX IF NOT EXISTS markers_auto_uidx
    ON markers (video_id, source, (round(timestamp::numeric, 0)), source_ref)
    WHERE source <> 'user';

ALTER TABLE watched_channels ADD COLUMN IF NOT EXISTS channel_id UUID REFERENCES channels(id) ON DELETE SET NULL;

-- Materialize channels from existing videos.
INSERT INTO channels (platform, identity_key, channel_id, uploader, canonical_url, search)
SELECT DISTINCT ON (platform, identity_key)
    platform,
    identity_key,
    channel_id,
    uploader,
    canonical_url,
    setweight(to_tsvector('simple', coalesce(uploader, '')), 'A')
FROM (
    SELECT
        CASE
            WHEN v.src ILIKE '%youtube.com%' OR v.src ILIKE '%youtu.be%' THEN 'youtube'
            WHEN v.src ILIKE '%rumble.com%' THEN 'rumble'
            WHEN v.src ILIKE '%vimeo.com%' THEN 'vimeo'
            WHEN v.src ILIKE '%kick.com%' THEN 'kick'
            WHEN v.src ILIKE '%twitch.tv%' THEN 'twitch'
            ELSE 'other'
        END AS platform,
        COALESCE(
            NULLIF(btrim(COALESCE(v.channel_id, '')), ''),
            NULLIF(btrim(COALESCE(v.uploader_id, '')), ''),
            NULLIF(btrim(COALESCE(v.channel_url, '')), ''),
            NULLIF(btrim(v.uploader), ''),
            'unknown'
        ) AS identity_key,
        COALESCE(v.channel_id, '') AS channel_id,
        v.uploader,
        COALESCE(NULLIF(v.channel_url, ''), NULLIF(v.uploader_url, ''), v.src) AS canonical_url
    FROM videos v
) s
ORDER BY platform, identity_key, uploader;

UPDATE videos v
SET channel_row_id = c.id
FROM channels c
WHERE c.platform = CASE
        WHEN v.src ILIKE '%youtube.com%' OR v.src ILIKE '%youtu.be%' THEN 'youtube'
        WHEN v.src ILIKE '%rumble.com%' THEN 'rumble'
        WHEN v.src ILIKE '%vimeo.com%' THEN 'vimeo'
        WHEN v.src ILIKE '%kick.com%' THEN 'kick'
        WHEN v.src ILIKE '%twitch.tv%' THEN 'twitch'
        ELSE 'other'
    END
  AND c.identity_key = COALESCE(
        NULLIF(btrim(COALESCE(v.channel_id, '')), ''),
        NULLIF(btrim(COALESCE(v.uploader_id, '')), ''),
        NULLIF(btrim(COALESCE(v.channel_url, '')), ''),
        NULLIF(btrim(v.uploader), ''),
        'unknown'
      );

UPDATE videos SET format = CASE
    WHEN COALESCE((info->>'was_live')::boolean, false)
      OR lower(COALESCE(info->>'live_status', '')) IN ('was_live', 'is_live', 'post_live')
      THEN 'livestream'
    WHEN src ILIKE '%/shorts/%' OR src ILIKE '%/short/%' THEN 'short'
    WHEN COALESCE(duration_seconds, 0) > 0 AND duration_seconds <= 60 THEN 'short'
    ELSE 'video'
END;

-- +goose Down
ALTER TABLE watched_channels DROP COLUMN IF EXISTS channel_id;
DROP INDEX IF EXISTS markers_auto_uidx;
ALTER TABLE markers DROP COLUMN IF EXISTS source_ref;
ALTER TABLE markers DROP COLUMN IF EXISTS source;
DROP INDEX IF EXISTS videos_format_idx;
DROP INDEX IF EXISTS videos_channel_row_id_idx;
ALTER TABLE videos DROP COLUMN IF EXISTS metadata_refreshed_at;
ALTER TABLE videos DROP COLUMN IF EXISTS format;
ALTER TABLE videos DROP COLUMN IF EXISTS channel_row_id;
DROP TABLE IF EXISTS channel_edges;
DROP TABLE IF EXISTS channels;
DROP TABLE IF EXISTS creators;
