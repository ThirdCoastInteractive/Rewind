-- +goose Up
-- Partial index for ListVideosForAssetCatchup: videos with a path whose
-- generated assets look incomplete, ordered by updated_at. The query's
-- error-backoff predicate (NOW()-relative) is applied after the index.
CREATE INDEX IF NOT EXISTS videos_asset_catchup_updated_at_idx
ON videos (updated_at)
WHERE video_path IS NOT NULL AND btrim(video_path) <> ''
AND (
    lower(video_path) NOT LIKE '%.mp4'
    OR assets_status = '{}'::jsonb
    OR NOT (assets_status ?& array['thumbnail','preview','waveform','file_hash','seek','faststart','captions_clean'])
    OR assets_status @> '{"thumbnail": false}'::jsonb
    OR assets_status @> '{"preview": false}'::jsonb
    OR assets_status @> '{"waveform": false}'::jsonb
    OR assets_status @> '{"file_hash": false}'::jsonb
    OR assets_status @> '{"seek": false}'::jsonb
    OR assets_status @> '{"faststart": false}'::jsonb
    OR assets_status @> '{"captions_clean": false}'::jsonb
);

-- +goose Down
DROP INDEX IF EXISTS videos_asset_catchup_updated_at_idx;
