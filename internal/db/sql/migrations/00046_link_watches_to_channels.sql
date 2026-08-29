-- +goose Up
-- Attach existing channel-follows (watched_channels) to first-class channel rows
-- when the watch URL or label matches a materialized channel.
UPDATE watched_channels w
SET channel_id = c.id,
    updated_at = NOW()
FROM channels c
WHERE w.channel_id IS NULL
  AND (
    (c.canonical_url <> '' AND c.canonical_url = w.url)
    OR (btrim(w.label) <> '' AND c.uploader = w.label)
  );

-- +goose Down
UPDATE watched_channels SET channel_id = NULL WHERE channel_id IS NOT NULL;
