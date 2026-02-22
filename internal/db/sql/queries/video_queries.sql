-- InsertVideo inserts a video row.
-- name: InsertVideo :one
INSERT INTO videos (
    id,
    src,
    archived_by,
    title,
    thumb_gradient_start,
    thumb_gradient_end,
    thumb_gradient_angle,
    description,
    tags,
    uploader,
    uploader_id,
    channel_id,
    upload_date,
    duration_seconds,
    view_count,
    like_count,
    info,
    comments,
    video_path,
    thumbnail_path,
    file_hash,
    file_size,
    probe_data,
    search
)
VALUES (
    sqlc.arg(id),
    sqlc.arg(src),
    sqlc.arg(archived_by),
    sqlc.arg(title),
    sqlc.narg('thumb_gradient_start'),
    sqlc.narg('thumb_gradient_end'),
    sqlc.narg('thumb_gradient_angle'),
    sqlc.arg(description),
    sqlc.arg(tags),
    sqlc.arg(uploader),
    sqlc.narg('uploader_id'),
    sqlc.narg('channel_id'),
    CASE
        WHEN (sqlc.narg('upload_date')::text) IS NULL OR btrim(sqlc.narg('upload_date')::text) = '' THEN NULL
        WHEN (sqlc.narg('upload_date')::text) ~ '^[0-9]{8}$' THEN to_date(sqlc.narg('upload_date')::text, 'YYYYMMDD')
        ELSE (sqlc.narg('upload_date')::text)::date
    END,
    sqlc.narg('duration_seconds'),
    sqlc.narg('view_count'),
    sqlc.narg('like_count'),
    sqlc.arg(info),
    sqlc.arg(comments),
    sqlc.arg(video_path),
    sqlc.arg(thumbnail_path),
    sqlc.arg(file_hash),
    sqlc.arg(file_size),
    sqlc.arg(probe_data),
    to_tsvector('simple'::regconfig,
        coalesce(sqlc.arg(title), '') || ' ' ||
        coalesce(sqlc.arg(description), '') || ' ' ||
        coalesce(array_to_string(sqlc.arg(tags)::text[], ' '), '')
    )
)
ON CONFLICT (src)
DO UPDATE SET
    updated_at = NOW(),
    title = EXCLUDED.title,
    thumb_gradient_start = COALESCE(EXCLUDED.thumb_gradient_start, videos.thumb_gradient_start),
    thumb_gradient_end = COALESCE(EXCLUDED.thumb_gradient_end, videos.thumb_gradient_end),
    thumb_gradient_angle = COALESCE(EXCLUDED.thumb_gradient_angle, videos.thumb_gradient_angle),
    description = EXCLUDED.description,
    tags = EXCLUDED.tags,
    uploader = EXCLUDED.uploader,
    uploader_id = EXCLUDED.uploader_id,
    channel_id = EXCLUDED.channel_id,
    upload_date = EXCLUDED.upload_date,
    duration_seconds = EXCLUDED.duration_seconds,
    view_count = EXCLUDED.view_count,
    like_count = EXCLUDED.like_count,
    info = EXCLUDED.info,
    comments = EXCLUDED.comments,
    video_path = EXCLUDED.video_path,
    thumbnail_path = EXCLUDED.thumbnail_path,
    file_hash = EXCLUDED.file_hash,
    file_size = EXCLUDED.file_size,
    probe_data = COALESCE(EXCLUDED.probe_data, videos.probe_data),
    search = EXCLUDED.search
RETURNING *;

-- SelectVideoBySrc returns a video by src.
-- name: SelectVideoBySrc :one
SELECT *
FROM videos
WHERE src = $1;

-- DeleteVideo deletes a video by ID (cascades to related records).
-- name: DeleteVideo :exec
DELETE FROM videos
WHERE id = $1;

-- ClearVideoFromJobs sets video_id to NULL for all jobs referencing this video.
-- name: ClearVideoFromJobs :exec
UPDATE download_jobs
SET video_id = NULL,
    updated_at = NOW()
WHERE video_id = $1;

-- CountVideosWithVideoPath returns count of videos that have a video_path.
-- name: CountVideosWithVideoPath :one
SELECT COUNT(*)
FROM videos
WHERE video_path IS NOT NULL AND btrim(video_path) <> '';

-- ListVideosForAssetCatchup returns videos that are missing one or more generated assets.
-- Videos with recent errors are backed off exponentially based on _error_count.
-- name: ListVideosForAssetCatchup :many
SELECT id::text, video_path, thumbnail_path, file_hash, duration_seconds, assets_status
FROM videos
WHERE video_path IS NOT NULL AND btrim(video_path) <> ''
AND (
    assets_status = '{}'::jsonb
    OR NOT (assets_status ?& array['thumbnail','preview','waveform','file_hash','seek'])
    OR assets_status @> '{"thumbnail": false}'::jsonb
    OR assets_status @> '{"preview": false}'::jsonb
    OR assets_status @> '{"waveform": false}'::jsonb
    OR assets_status @> '{"file_hash": false}'::jsonb
    OR assets_status @> '{"seek": false}'::jsonb
)
AND (
    -- No errors yet, or backoff period has elapsed.
    -- Backoff: 2^error_count minutes (1=2m, 2=4m, 3=8m, ... 10+=~17h cap).
    NOT (assets_status ? '_error_count')
    OR (assets_status->>'_error_count')::int < 1
    OR (
        assets_status ? '_last_error_at'
        AND (assets_status->>'_last_error_at')::timestamptz
            + (LEAST(POWER(2, LEAST((assets_status->>'_error_count')::int, 10)), 1024) || ' minutes')::interval
            < NOW()
    )
)
ORDER BY updated_at ASC
LIMIT $1;

-- ListVideosWithAssetErrors returns videos that have recorded asset generation errors.
-- name: ListVideosWithAssetErrors :many
SELECT id::text, title, video_path, assets_status, updated_at
FROM videos
WHERE video_path IS NOT NULL AND btrim(video_path) <> ''
AND assets_status ? '_error_count'
AND (assets_status->>'_error_count')::int > 0
ORDER BY (assets_status->>'_last_error_at')::timestamptz DESC NULLS LAST
LIMIT $1;

-- CountVideosWithAssetErrors returns the number of videos with asset generation errors.
-- name: CountVideosWithAssetErrors :one
SELECT COUNT(*)
FROM videos
WHERE video_path IS NOT NULL AND btrim(video_path) <> ''
AND assets_status ? '_error_count'
AND (assets_status->>'_error_count')::int > 0;

-- ClearVideoAssetErrors resets error tracking for a single video so catchup retries it.
-- name: ClearVideoAssetErrors :exec
UPDATE videos
SET assets_status = assets_status - '_error_count' - '_last_error_at' - '_errors',
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- ClearAllVideoAssetErrors resets error tracking for all videos so catchup retries them.
-- name: ClearAllVideoAssetErrors :exec
UPDATE videos
SET assets_status = assets_status - '_error_count' - '_last_error_at' - '_errors',
    updated_at = NOW()
WHERE assets_status ? '_error_count'
AND (assets_status->>'_error_count')::int > 0;

-- UpdateVideoPath updates the video_path for a video.
-- name: UpdateVideoPath :exec
UPDATE videos
SET video_path = sqlc.arg(video_path),
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- UpdateVideoThumbnailPath updates the thumbnail_path for a video.
-- name: UpdateVideoThumbnailPath :exec
UPDATE videos
SET thumbnail_path = sqlc.arg(thumbnail_path),
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- UpdateVideoFileHashAndSize updates file_hash + file_size for a video.
-- name: UpdateVideoFileHashAndSize :exec
UPDATE videos
SET file_hash = sqlc.arg(file_hash),
    file_size = sqlc.arg(file_size),
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- UpdateVideoProbeData stores ffprobe data for a video.
-- name: UpdateVideoProbeData :exec
UPDATE videos
SET probe_data = sqlc.arg(probe_data),
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- ListVideosNeedingProbe returns videos with a video_path but no probe_data, for backfill.
-- name: ListVideosNeedingProbe :many
SELECT id, video_path
FROM videos
WHERE video_path IS NOT NULL AND btrim(video_path) <> ''
  AND probe_data IS NULL
ORDER BY created_at DESC
LIMIT sqlc.arg(max_count);

-- UpdateVideoAssetsStatus merges asset status flags into videos.assets_status.
-- name: UpdateVideoAssetsStatus :exec
UPDATE videos
SET assets_status = COALESCE(assets_status, '{}'::jsonb) || sqlc.arg(assets_status)::asset_status_map,
    updated_at = NOW()
WHERE id = sqlc.arg(id);
