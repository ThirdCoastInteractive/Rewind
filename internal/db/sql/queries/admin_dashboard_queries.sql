-- ============================================================================
-- Admin Dashboard Metrics
-- ============================================================================

-- GetDashboardOverview returns high-level counts and totals for the admin dashboard.
-- name: GetDashboardOverview :one
SELECT
    (SELECT COUNT(*)::bigint FROM videos) AS total_videos,
    (SELECT COUNT(*)::bigint FROM clips) AS total_clips,
    (SELECT COUNT(*)::bigint FROM markers) AS total_markers,
    (SELECT COUNT(*)::bigint FROM users WHERE deleted_at IS NULL) AS total_users,
    (SELECT COUNT(*)::bigint FROM video_comments) AS total_comments,
    (SELECT COALESCE(SUM(file_size), 0)::bigint FROM videos WHERE file_size IS NOT NULL) AS total_storage_bytes,
    (SELECT COALESCE(SUM(duration_seconds), 0)::bigint FROM videos WHERE duration_seconds IS NOT NULL) AS total_duration_seconds;

-- GetJobStatusCounts returns download and ingest job counts grouped by status.
-- name: GetJobStatusCounts :many
SELECT
    'download' AS job_type,
    status::text AS status,
    COUNT(*)::bigint AS count
FROM download_jobs
GROUP BY status
UNION ALL
SELECT
    'ingest' AS job_type,
    status::text AS status,
    COUNT(*)::bigint AS count
FROM ingest_jobs
GROUP BY status
ORDER BY job_type, status;

-- GetVideosPerDay returns the number of videos archived per day for the last N days.
-- name: GetVideosPerDay :many
SELECT
    d.day::date AS day,
    COUNT(v.id)::bigint AS count
FROM generate_series(
    CURRENT_DATE - (sqlc.arg(days)::int - 1) * INTERVAL '1 day',
    CURRENT_DATE,
    '1 day'
) AS d(day)
LEFT JOIN videos v ON v.created_at::date = d.day::date
GROUP BY d.day
ORDER BY d.day;

-- GetTopSources returns the top video sources (platforms) by count.
-- name: GetTopSources :many
SELECT
    COALESCE(
        regexp_replace(src, 'https?://(?:www\.)?([^/]+).*', '\1'),
        'unknown'
    )::text AS source,
    COUNT(*)::bigint AS count
FROM videos
GROUP BY source
ORDER BY count DESC
LIMIT 10;

-- GetStorageByUploader returns total file storage grouped by uploader.
-- name: GetStorageByUploader :many
SELECT
    (CASE WHEN uploader = '' THEN 'unknown' ELSE uploader END)::text AS uploader,
    COALESCE(SUM(file_size), 0)::bigint AS total_bytes
FROM videos
WHERE file_size IS NOT NULL
GROUP BY uploader
ORDER BY total_bytes DESC
LIMIT 10;
