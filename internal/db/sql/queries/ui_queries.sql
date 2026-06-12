-- ListDownloadJobsByUser returns all download jobs for a user
-- name: ListDownloadJobsByUser :many
SELECT *
FROM download_jobs
WHERE archived_by = sqlc.arg(archived_by)
  AND archived = FALSE
ORDER BY created_at DESC
LIMIT sqlc.arg(page_limit);

-- ListRecentDownloadJobs returns recent download jobs for all users
-- name: ListRecentDownloadJobs :many
SELECT *
FROM download_jobs
WHERE archived = FALSE
ORDER BY created_at DESC
LIMIT 100;

-- GetDownloadJobByID returns a download job by ID
-- name: GetDownloadJobByID :one
SELECT *
FROM download_jobs
WHERE id = sqlc.arg(id);

-- ListVideosPaginated returns videos with filters, sorting, and pagination.
-- Returns total_count via window function for pagination UI.
-- name: ListVideosPaginated :many
SELECT 
    v.*,
    COUNT(*) OVER() AS total_count,
    COALESCE((SELECT COUNT(*) FROM clips c WHERE c.video_id = v.id), 0) AS clip_count,
    COALESCE((SELECT COUNT(*) FROM markers m WHERE m.video_id = v.id), 0) AS marker_count,
    COALESCE((SELECT MAX(c.created_at) FROM clips c WHERE c.video_id = v.id), '1970-01-01'::timestamptz) AS last_clip_at,
    COALESCE((SELECT MAX(m.created_at) FROM markers m WHERE m.video_id = v.id), '1970-01-01'::timestamptz) AS last_marker_at,
    COALESCE(u.user_name, 'unknown') AS archived_by_username
FROM videos v
LEFT JOIN users u ON v.archived_by = u.id
WHERE
    -- Full-text search (optional)
    (sqlc.narg('query')::text IS NULL OR v.search @@ plainto_tsquery('simple', sqlc.narg('query')))
    -- Uploader filter (optional)
    AND (sqlc.narg('uploader')::text IS NULL OR v.uploader = sqlc.narg('uploader'))
    -- Channel filter (optional)
    AND (sqlc.narg('channel_id')::text IS NULL OR v.channel_id = sqlc.narg('channel_id'))
    -- Duration filter: short=<5min, medium=5-30min, long=>30min
    AND (
        sqlc.narg('duration_filter')::text IS NULL
        OR (sqlc.narg('duration_filter') = 'short' AND v.duration_seconds < 300)
        OR (sqlc.narg('duration_filter') = 'medium' AND v.duration_seconds >= 300 AND v.duration_seconds < 1800)
        OR (sqlc.narg('duration_filter') = 'long' AND v.duration_seconds >= 1800)
    )
    -- Scraped tags filter (any tag matches)
    AND (sqlc.narg('tags')::text[] IS NULL OR v.tags && sqlc.narg('tags')::text[])
    -- User tag filter (video has any of the selected tag ids)
    AND (sqlc.narg('tag_ids')::uuid[] IS NULL OR EXISTS (
        SELECT 1 FROM video_tags vt
        WHERE vt.video_id = v.id AND vt.tag_id = ANY(sqlc.narg('tag_ids')::uuid[])
    ))
    -- Date range (archived or published based on date_type)
    AND (
        sqlc.narg('date_from')::date IS NULL 
        OR (sqlc.narg('date_type')::text = 'published' AND v.upload_date >= sqlc.narg('date_from'))
        OR (sqlc.narg('date_type')::text IS DISTINCT FROM 'published' AND v.created_at::date >= sqlc.narg('date_from'))
    )
    AND (
        sqlc.narg('date_to')::date IS NULL
        OR (sqlc.narg('date_type')::text = 'published' AND v.upload_date <= sqlc.narg('date_to'))
        OR (sqlc.narg('date_type')::text IS DISTINCT FROM 'published' AND v.created_at::date <= sqlc.narg('date_to'))
    )
    -- Has clips filter
    AND (sqlc.narg('has_clips')::boolean IS NULL OR sqlc.narg('has_clips') = FALSE 
         OR EXISTS (SELECT 1 FROM clips c WHERE c.video_id = v.id))
    -- Has markers filter
    AND (sqlc.narg('has_markers')::boolean IS NULL OR sqlc.narg('has_markers') = FALSE
         OR EXISTS (SELECT 1 FROM markers m WHERE m.video_id = v.id))
ORDER BY
    -- Date sorts (archived)
    CASE WHEN sqlc.arg(sort_order) = 'newest' THEN v.created_at END DESC NULLS LAST,
    CASE WHEN sqlc.arg(sort_order) = 'oldest' THEN v.created_at END ASC NULLS LAST,
    -- Date sorts (published)
    CASE WHEN sqlc.arg(sort_order) = 'published-newest' THEN v.upload_date END DESC NULLS LAST,
    CASE WHEN sqlc.arg(sort_order) = 'published-oldest' THEN v.upload_date END ASC NULLS LAST,
    -- Title sorts
    CASE WHEN sqlc.arg(sort_order) = 'alpha' THEN v.title END ASC NULLS LAST,
    CASE WHEN sqlc.arg(sort_order) = 'alpha-desc' THEN v.title END DESC NULLS LAST,
    -- Duration sorts
    CASE WHEN sqlc.arg(sort_order) = 'duration' THEN v.duration_seconds END ASC NULLS LAST,
    CASE WHEN sqlc.arg(sort_order) = 'duration-desc' THEN v.duration_seconds END DESC NULLS LAST,
    -- Activity sorts
    CASE WHEN sqlc.arg(sort_order) = 'most-clips' THEN (SELECT COUNT(*) FROM clips c WHERE c.video_id = v.id) END DESC NULLS LAST,
    CASE WHEN sqlc.arg(sort_order) = 'most-markers' THEN (SELECT COUNT(*) FROM markers m WHERE m.video_id = v.id) END DESC NULLS LAST,
    CASE WHEN sqlc.arg(sort_order) = 'recently-clipped' THEN (SELECT MAX(c.created_at) FROM clips c WHERE c.video_id = v.id) END DESC NULLS LAST,
    CASE WHEN sqlc.arg(sort_order) = 'recently-marked' THEN (SELECT MAX(m.created_at) FROM markers m WHERE m.video_id = v.id) END DESC NULLS LAST,
    -- Default fallback
    v.created_at DESC
LIMIT sqlc.arg(page_limit)
OFFSET sqlc.arg(page_offset);

-- ListDistinctUploaders returns unique uploader names for filter dropdown
-- name: ListDistinctUploaders :many
SELECT DISTINCT uploader
FROM videos
WHERE uploader IS NOT NULL AND uploader != ''
ORDER BY uploader ASC
LIMIT 100;

-- ListDistinctTags returns unique tags for filter dropdown
-- name: ListDistinctTags :many
SELECT DISTINCT unnest(tags) AS tag
FROM videos
WHERE tags IS NOT NULL AND array_length(tags, 1) > 0
ORDER BY tag ASC
LIMIT 200;

-- ListRecentVideos returns recent videos (by archive date)
-- name: ListRecentVideos :many
SELECT *
FROM videos
ORDER BY created_at DESC
LIMIT 15;

-- ListRecentlyPublishedVideos returns videos sorted by original publish date
-- name: ListRecentlyPublishedVideos :many
SELECT *
FROM videos
WHERE upload_date IS NOT NULL
ORDER BY upload_date DESC
LIMIT 15;

-- GetHomeStats returns aggregate stats for the home page dashboard
-- name: GetHomeStats :one
SELECT
    (SELECT COUNT(*)::bigint FROM videos) AS video_count,
    (SELECT COUNT(*)::bigint FROM clips) AS clip_count,
    (SELECT COUNT(*)::bigint FROM stitch_projects) AS stitch_count,
    (SELECT COALESCE(SUM(file_size), 0)::bigint FROM videos WHERE file_size IS NOT NULL) AS storage_bytes,
    (SELECT COALESCE(SUM(duration_seconds), 0)::bigint FROM videos WHERE duration_seconds IS NOT NULL) AS total_duration_seconds;

-- ListRecentClips returns recently created clips with their source video title
-- name: ListRecentClips :many
SELECT
    c.id,
    c.video_id,
    c.title AS clip_title,
    c.start_ts,
    c.end_ts,
    c.duration,
    c.color,
    c.created_at,
    v.title AS video_title
FROM clips c
JOIN videos v ON v.id = c.video_id
ORDER BY c.created_at DESC
LIMIT 8;

-- GetVideoByID returns a video by ID
-- name: GetVideoByID :one
SELECT *
FROM videos
WHERE id = sqlc.arg(id);

-- ListDownloadJobsByVideoID returns all download jobs for a video.
-- Matches by video_id FK or by URL matching the video's src column.
-- name: ListDownloadJobsByVideoID :many
SELECT *
FROM download_jobs
WHERE video_id = sqlc.arg(video_id)
   OR url = sqlc.arg(video_src)
ORDER BY created_at DESC;

-- ListIngestJobsByDownloadJobIDs returns ingest jobs for a set of download job IDs.
-- name: ListIngestJobsByDownloadJobIDs :many
SELECT *
FROM ingest_jobs
WHERE download_job_id = ANY(sqlc.arg(download_job_ids)::uuid[])
ORDER BY created_at DESC;
