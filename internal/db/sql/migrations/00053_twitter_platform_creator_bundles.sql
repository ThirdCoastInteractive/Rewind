-- +goose Up

-- Twitter/X channels were materialized as platform='other' (migration 00044
-- had no twitter branch). Flip them without scraping X. Merge rows that
-- already exist as platform='twitter' with the same identity_key.

UPDATE videos v
SET channel_row_id = t.id
FROM channels o
JOIN channels t
  ON t.identity_key = o.identity_key
 AND t.platform = 'twitter'
 AND t.id <> o.id
WHERE o.platform = 'other'
  AND o.canonical_url ~* 'https?://(www\.)?(twitter|x)\.com/'
  AND v.channel_row_id = o.id;

UPDATE watched_channels w
SET channel_id = t.id
FROM channels o
JOIN channels t
  ON t.identity_key = o.identity_key
 AND t.platform = 'twitter'
 AND t.id <> o.id
WHERE o.platform = 'other'
  AND o.canonical_url ~* 'https?://(www\.)?(twitter|x)\.com/'
  AND w.channel_id = o.id;

UPDATE channel_edges e
SET to_channel_id = t.id
FROM channels o
JOIN channels t
  ON t.identity_key = o.identity_key
 AND t.platform = 'twitter'
 AND t.id <> o.id
WHERE o.platform = 'other'
  AND o.canonical_url ~* 'https?://(www\.)?(twitter|x)\.com/'
  AND e.to_channel_id = o.id
  AND NOT EXISTS (
      SELECT 1 FROM channel_edges e2
      WHERE e2.from_channel_id = e.from_channel_id
        AND e2.kind = e.kind
        AND e2.id <> e.id
        AND COALESCE(e2.to_channel_id::text, e2.to_url) = t.id::text
  );

UPDATE channel_edges e
SET from_channel_id = t.id
FROM channels o
JOIN channels t
  ON t.identity_key = o.identity_key
 AND t.platform = 'twitter'
 AND t.id <> o.id
WHERE o.platform = 'other'
  AND o.canonical_url ~* 'https?://(www\.)?(twitter|x)\.com/'
  AND e.from_channel_id = o.id
  AND NOT EXISTS (
      SELECT 1 FROM channel_edges e2
      WHERE e2.from_channel_id = t.id
        AND e2.kind = e.kind
        AND e2.id <> e.id
        AND COALESCE(e2.to_channel_id::text, e2.to_url) = COALESCE(e.to_channel_id::text, e.to_url)
  );

-- Keep creator assignment / labels from the duplicate 'other' row.
UPDATE channels t
SET
    creator_id = COALESCE(t.creator_id, o.creator_id),
    uploader = CASE WHEN btrim(t.uploader) = '' THEN o.uploader ELSE t.uploader END,
    canonical_url = CASE WHEN btrim(t.canonical_url) = '' THEN o.canonical_url ELSE t.canonical_url END,
    updated_at = NOW()
FROM channels o
WHERE o.platform = 'other'
  AND t.platform = 'twitter'
  AND t.identity_key = o.identity_key
  AND t.id <> o.id
  AND o.canonical_url ~* 'https?://(www\.)?(twitter|x)\.com/';

DELETE FROM channels o
USING channels t
WHERE o.platform = 'other'
  AND t.platform = 'twitter'
  AND t.identity_key = o.identity_key
  AND t.id <> o.id
  AND o.canonical_url ~* 'https?://(www\.)?(twitter|x)\.com/';

UPDATE channels
SET platform = 'twitter', updated_at = NOW()
WHERE platform = 'other'
  AND canonical_url ~* 'https?://(www\.)?(twitter|x)\.com/';

-- Profile URLs on X are handles, not scrape targets. Keep them as mentions.
DELETE FROM channel_edges e
WHERE e.kind = 'outlink'
  AND e.to_url ~* 'https?://(www\.)?(twitter|x)\.com/'
  AND EXISTS (
      SELECT 1 FROM channel_edges m
      WHERE m.from_channel_id = e.from_channel_id
        AND m.kind = 'mention'
        AND m.id <> e.id
        AND COALESCE(m.to_channel_id::text, m.to_url) = COALESCE(e.to_channel_id::text, e.to_url)
  );

UPDATE channel_edges
SET kind = 'mention', updated_at = NOW()
WHERE kind = 'outlink'
  AND to_url ~* 'https?://(www\.)?(twitter|x)\.com/';

CREATE TABLE creator_bundles (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    name       TEXT NOT NULL,
    notes      TEXT NOT NULL DEFAULT '',
    search     TSVECTOR NOT NULL DEFAULT ''
);
CREATE INDEX creator_bundles_search_gin ON creator_bundles USING GIN (search);
CREATE INDEX creator_bundles_name_trgm ON creator_bundles USING GIN (name gin_trgm_ops);

CREATE TABLE creator_bundle_members (
    bundle_id  UUID NOT NULL REFERENCES creator_bundles(id) ON DELETE CASCADE,
    creator_id UUID NOT NULL REFERENCES creators(id) ON DELETE CASCADE,
    PRIMARY KEY (bundle_id, creator_id)
);
CREATE INDEX creator_bundle_members_creator_idx ON creator_bundle_members (creator_id);

-- +goose Down
DROP TABLE IF EXISTS creator_bundle_members;
DROP TABLE IF EXISTS creator_bundles;
