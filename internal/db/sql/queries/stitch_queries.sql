-- name: SearchClipsForStitch :many
-- Cross-video clip search for the stitch clip browser.
SELECT c.id, c.video_id, c.start_ts, c.end_ts, c.duration,
       c.title, c.color, c.created_at,
       v.title AS video_title
FROM clips c
JOIN videos v ON c.video_id = v.id
WHERE (sqlc.arg(query)::text = '' OR c.title ILIKE '%' || sqlc.arg(query) || '%' OR v.title ILIKE '%' || sqlc.arg(query) || '%')
ORDER BY
    CASE WHEN sqlc.arg(sort_by)::text = 'alpha'    THEN c.title     END ASC,
    CASE WHEN sqlc.arg(sort_by)::text = 'duration' THEN c.duration  END DESC,
    c.created_at DESC
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);

-- name: GetClipsForStitch :many
-- Bulk load clip data for the encoder (timestamps, crops).
SELECT c.id, c.video_id, c.start_ts, c.end_ts, c.duration, c.crops, c.filter_stack
FROM clips c
WHERE c.id = ANY(sqlc.arg(ids)::uuid[]);

-- name: CreateStitchJob :one
INSERT INTO stitch_jobs (created_by, title, format, quality, segments, global_filters, project_id)
VALUES (sqlc.arg(created_by), sqlc.arg(title), sqlc.arg(format), sqlc.arg(quality),
        sqlc.arg(segments), sqlc.arg(global_filters), sqlc.narg(project_id))
RETURNING id;

-- name: GetStitchJob :one
SELECT id, title, format, quality, segments, global_filters, status, progress_pct,
       file_path, size_bytes, last_error, created_at, updated_at
FROM stitch_jobs
WHERE id = sqlc.arg(id);

-- name: GetStitchJobStatus :one
SELECT id, status, progress_pct, file_path, last_error
FROM stitch_jobs
WHERE id = sqlc.arg(id);

-- name: FindAndLockPendingStitchJob :one
-- Atomically claim the oldest queued stitch job for processing.
UPDATE stitch_jobs
SET status     = 'processing',
    locked_at  = NOW(),
    locked_by  = sqlc.arg(locked_by),
    started_at = NOW(),
    attempts   = attempts + 1,
    updated_at = NOW()
WHERE id = (
    SELECT id FROM stitch_jobs
    WHERE status = 'queued'
      AND (locked_at IS NULL OR locked_at < NOW() - INTERVAL '10 minutes')
    ORDER BY created_at ASC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING id, created_by, title, format, quality, segments, global_filters;

-- name: UpdateStitchJobPID :exec
UPDATE stitch_jobs
SET pid = sqlc.arg(pid), updated_at = NOW()
WHERE id = sqlc.arg(id);

-- name: UpdateStitchJobProgress :exec
UPDATE stitch_jobs
SET progress_pct = sqlc.arg(progress_pct), updated_at = NOW()
WHERE id = sqlc.arg(id);

-- name: FinishStitchJobReady :exec
UPDATE stitch_jobs
SET status           = 'ready',
    file_path        = sqlc.arg(file_path),
    size_bytes       = sqlc.arg(size_bytes),
    duration_seconds = sqlc.narg(duration_seconds),
    progress_pct     = 100,
    finished_at      = NOW(),
    locked_at        = NULL,
    locked_by        = NULL,
    pid              = NULL,
    last_accessed_at = NOW(),
    updated_at       = NOW()
WHERE id = sqlc.arg(id);

-- name: FinishStitchJobError :exec
UPDATE stitch_jobs
SET status      = 'error',
    last_error  = sqlc.arg(last_error),
    finished_at = NOW(),
    locked_at   = NULL,
    locked_by   = NULL,
    pid         = NULL,
    updated_at  = NOW()
WHERE id = sqlc.arg(id);

-- name: ResetStuckStitchJobs :exec
-- Reset stitch jobs stuck in processing without recent progress.
UPDATE stitch_jobs
SET status     = 'queued',
    locked_at  = NULL,
    locked_by  = NULL,
    progress_pct = 0,
    updated_at = NOW()
WHERE status = 'processing'
  AND updated_at < NOW() - INTERVAL '10 minutes';

-- name: UpdateStitchJobLastAccessed :exec
UPDATE stitch_jobs
SET last_accessed_at = NOW(), updated_at = NOW()
WHERE id = sqlc.arg(id);

-- ============================================================================
-- Stitch projects (persistent editor state)
-- ============================================================================

-- name: ListStitchProjects :many
-- List all stitch projects for a user, newest-updated first.
SELECT id, title, format, quality, segments, created_at, updated_at
FROM stitch_projects
WHERE created_by = sqlc.arg(user_id)
ORDER BY updated_at DESC;

-- name: GetStitchProject :one
SELECT id, created_by, title, format, quality, segments, global_filters, created_at, updated_at
FROM stitch_projects
WHERE id = sqlc.arg(id);

-- name: CreateStitchProject :one
INSERT INTO stitch_projects (created_by, title)
VALUES (sqlc.arg(created_by), sqlc.arg(title))
RETURNING id;

-- name: UpdateStitchProject :exec
UPDATE stitch_projects
SET title          = sqlc.arg(title),
    format         = sqlc.arg(format),
    quality        = sqlc.arg(quality),
    segments       = sqlc.arg(segments),
    global_filters = sqlc.arg(global_filters),
    updated_at     = NOW()
WHERE id = sqlc.arg(id)
  AND created_by = sqlc.arg(user_id);

-- name: DeleteStitchProject :exec
DELETE FROM stitch_projects
WHERE id = sqlc.arg(id)
  AND created_by = sqlc.arg(user_id);

-- name: ListStitchJobsByProject :many
-- List all stitch jobs for a project, newest first. Used to show export history.
SELECT id, title, status, progress_pct, file_path, size_bytes, last_error, created_at, finished_at
FROM stitch_jobs
WHERE project_id = sqlc.arg(project_id)
ORDER BY created_at DESC
LIMIT 20;

-- name: LatestStitchJobPerProject :many
-- Get the latest stitch job for each project (for library cards).
SELECT DISTINCT ON (project_id)
    project_id, id, status, progress_pct, file_path, created_at
FROM stitch_jobs
WHERE project_id = ANY(sqlc.arg(project_ids)::uuid[])
ORDER BY project_id, created_at DESC;

-- ============================================================================
-- Universal source browser (unified search across clips, videos, exports)
-- ============================================================================

-- name: SearchSourcesForStitch :many
-- Combined search across clips, videos, and completed exports.
-- Returns a unified result set with a source_type discriminator.
SELECT * FROM (
    -- Clips
    SELECT 'clip'::text AS source_type,
           c.id AS source_id,
           c.video_id,
           c.title,
           v.title AS parent_title,
           c.duration,
           c.start_ts,
           c.end_ts,
           c.color,
           c.created_at,
           ''::text AS file_path
    FROM clips c
    JOIN videos v ON c.video_id = v.id
    WHERE (sqlc.arg(source_filter)::text = '' OR sqlc.arg(source_filter)::text = 'all' OR sqlc.arg(source_filter)::text = 'clip')
      AND (sqlc.arg(query)::text = '' OR c.title ILIKE '%' || sqlc.arg(query) || '%' OR v.title ILIKE '%' || sqlc.arg(query) || '%')

    UNION ALL

    -- Videos
    SELECT 'video'::text AS source_type,
           v.id AS source_id,
           v.id AS video_id,
           v.title,
           v.uploader AS parent_title,
           COALESCE(v.duration_seconds, 0)::float8 AS duration,
           0::float8 AS start_ts,
           COALESCE(v.duration_seconds, 0)::float8 AS end_ts,
           ''::text AS color,
           v.created_at,
           ''::text AS file_path
    FROM videos v
    WHERE (sqlc.arg(source_filter)::text = '' OR sqlc.arg(source_filter)::text = 'all' OR sqlc.arg(source_filter)::text = 'video')
      AND (sqlc.arg(query)::text = '' OR v.search @@ websearch_to_tsquery('english', sqlc.arg(query)))

    UNION ALL

    -- Stitch exports (ready only)
    SELECT 'stitch'::text AS source_type,
           sj.id AS source_id,
           NULL::uuid AS video_id,
           sj.title,
           sj.format AS parent_title,
           COALESCE(sj.duration_seconds, 0)::float8 AS duration,
           0::float8 AS start_ts,
           COALESCE(sj.duration_seconds, 0)::float8 AS end_ts,
           ''::text AS color,
           sj.created_at,
           sj.file_path
    FROM stitch_jobs sj
    WHERE sj.status = 'ready' AND sj.file_path != ''
      AND (sqlc.arg(source_filter)::text = '' OR sqlc.arg(source_filter)::text = 'all' OR sqlc.arg(source_filter)::text = 'stitch')
      AND (sqlc.arg(query)::text = '' OR sj.title ILIKE '%' || sqlc.arg(query) || '%')
) AS combined
ORDER BY
    CASE WHEN sqlc.arg(sort_by)::text = 'alpha'    THEN combined.title    END ASC,
    CASE WHEN sqlc.arg(sort_by)::text = 'duration' THEN combined.duration END DESC,
    combined.created_at DESC
LIMIT sqlc.arg(lim)
OFFSET sqlc.arg(off);

-- name: GetStitchExportFile :one
-- Lookup a completed stitch export for use as a source.
SELECT id, status, file_path, duration_seconds, title
FROM stitch_jobs
WHERE id = sqlc.arg(id);
