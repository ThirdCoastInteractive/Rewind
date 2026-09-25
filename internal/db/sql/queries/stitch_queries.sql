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
SELECT c.id, c.video_id, c.start_ts, c.end_ts, c.duration, c.crops, c.filter_stack, c.shot_list,
       v.video_path, v.tenant_id
FROM clips c
JOIN videos v ON v.id = c.video_id
WHERE c.id = ANY(sqlc.arg(ids)::uuid[]);

-- name: CreateStitchJob :one
INSERT INTO stitch_jobs (created_by, title, format, quality, segments, global_filters, project_id)
VALUES (sqlc.arg(created_by), sqlc.arg(title), sqlc.arg(format), sqlc.arg(quality),
        sqlc.arg(segments), sqlc.arg(global_filters), sqlc.narg(project_id))
RETURNING id;

-- name: GetStitchJob :one
SELECT id, created_by, title, format, quality, segments, global_filters, status, progress_pct,
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
RETURNING id, created_by, title, format, quality, segments, global_filters, document_snapshot, project_revision, render_options, render_kind, range_start_us, range_end_us, frame_time_us;

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

-- name: RequeueAllProcessingStitchJobs :exec
-- Startup recovery: this process died, so every processing row is orphaned.
UPDATE stitch_jobs
SET status     = 'queued',
    locked_at  = NULL,
    locked_by  = NULL,
    progress_pct = 0,
    updated_at = NOW()
WHERE status = 'processing';

-- name: ResetStuckStitchJobs :exec
-- Periodic recovery for jobs whose progress heartbeat (updated_at) went stale.
-- Long chapter encodes can run 40–55 minutes; progress updates refresh updated_at.
UPDATE stitch_jobs
SET status     = 'queued',
    locked_at  = NULL,
    locked_by  = NULL,
    progress_pct = 0,
    updated_at = NOW()
WHERE status = 'processing'
  AND updated_at < NOW() - INTERVAL '30 minutes';

-- name: UpdateStitchJobLastAccessed :exec
UPDATE stitch_jobs
SET last_accessed_at = NOW(), updated_at = NOW()
WHERE id = sqlc.arg(id);

-- ============================================================================
-- Stitch projects (persistent editor state)
-- ============================================================================

-- name: ListStitchProjects :many
-- List stitch projects. user_id NULL = every owner. folder_mode:
--   all     ignore folder
--   unfiled folder_id IS NULL
--   folder  folder_id = folder_id arg
SELECT p.id, p.title, p.format, p.quality, p.segments, p.created_at, p.updated_at,
       p.created_by, p.folder_id, u.user_name AS created_by_name,
       COALESCE(f.name, '') AS folder_name, p.description, p.tags
FROM stitch_projects p
JOIN users u ON u.id = p.created_by
LEFT JOIN stitch_folders f ON f.id = p.folder_id
WHERE (sqlc.narg(user_id)::uuid IS NULL OR p.created_by = sqlc.narg(user_id))
  AND (
    sqlc.arg(folder_mode)::text = 'all'
    OR (sqlc.arg(folder_mode)::text = 'unfiled' AND p.folder_id IS NULL)
    OR (sqlc.arg(folder_mode)::text = 'folder' AND p.folder_id = sqlc.narg(folder_id))
  )
  AND (sqlc.narg(query)::text IS NULL OR sqlc.narg(query) = '' OR p.title ILIKE '%' || sqlc.narg(query) || '%' OR p.description ILIKE '%' || sqlc.narg(query) || '%')
ORDER BY p.updated_at DESC;

-- name: ListStitchProjectOwners :many
SELECT u.id, u.user_name, COUNT(*)::bigint AS project_count
FROM stitch_projects p
JOIN users u ON u.id = p.created_by
WHERE u.deleted_at IS NULL
GROUP BY u.id, u.user_name
ORDER BY u.user_name;

-- name: ListStitchFoldersForUser :many
SELECT f.id, f.created_by, f.parent_id, f.name, f.created_at, f.updated_at,
       (SELECT COUNT(*)::bigint FROM stitch_projects p WHERE p.folder_id = f.id) AS project_count
FROM stitch_folders f
WHERE f.created_by = sqlc.arg(user_id)
ORDER BY f.name;

-- name: GetStitchFolder :one
SELECT id, created_by, parent_id, name, created_at, updated_at
FROM stitch_folders
WHERE id = sqlc.arg(id);

-- name: CreateStitchFolder :one
INSERT INTO stitch_folders (created_by, parent_id, name)
VALUES (sqlc.arg(created_by), sqlc.narg(parent_id), sqlc.arg(name))
RETURNING *;

-- name: RenameStitchFolder :exec
UPDATE stitch_folders
SET name = sqlc.arg(name), updated_at = now()
WHERE id = sqlc.arg(id) AND created_by = sqlc.arg(created_by);

-- name: DeleteStitchFolder :exec
DELETE FROM stitch_folders
WHERE id = sqlc.arg(id) AND created_by = sqlc.arg(created_by);

-- name: MoveStitchProject :exec
UPDATE stitch_projects
SET folder_id = sqlc.narg(folder_id), updated_at = now()
WHERE id = sqlc.arg(id) AND created_by = sqlc.arg(created_by);

-- name: GetStitchProject :one
SELECT id, created_by, title, format, quality, segments, global_filters, created_at, updated_at, description, tags
FROM stitch_projects
WHERE id = sqlc.arg(id);

-- name: UpdateStitchProjectYouTube :exec
UPDATE stitch_projects
SET description = sqlc.arg(description),
    tags = sqlc.arg(tags),
    updated_at = NOW()
WHERE id = sqlc.arg(id)
  AND created_by = sqlc.arg(created_by);

-- name: CreateStitchProject :one
-- New projects are always canonical documents (editor_enabled is set, never a product switch).
INSERT INTO stitch_projects (created_by, title, document, document_version, editor_enabled, revision)
VALUES (sqlc.arg(created_by), sqlc.arg(title), sqlc.arg(document), 1, true, 0)
RETURNING id;

-- name: CreateCompilationStitchProject :execrows
-- Deterministic ID per plan revision makes client retries safe.
INSERT INTO stitch_projects (id, created_by, title)
VALUES (sqlc.arg(id), sqlc.arg(created_by), sqlc.arg(title))
ON CONFLICT (id) DO NOTHING;

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
    WHERE (c.created_by = sqlc.arg(owner_id)::uuid) AND (sqlc.arg(source_filter)::text = '' OR sqlc.arg(source_filter)::text = 'all' OR sqlc.arg(source_filter)::text = 'clip')
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
      AND (sqlc.arg(query)::text = '' OR v.search @@ websearch_to_tsquery('simple', sqlc.arg(query))
           OR strpos(lower(v.title), lower(sqlc.arg(query))) > 0
           OR strpos(lower(v.uploader), lower(sqlc.arg(query))) > 0)

    UNION ALL

    -- Topic-bound context windows (playable ranges)
    SELECT 'context'::text AS source_type,
           cw.id AS source_id,
           cw.video_id,
           cw.title,
           t.title AS parent_title,
           (cw.end_ts - cw.start_ts)::float8 AS duration,
           cw.start_ts,
           cw.end_ts,
           ''::text AS color,
           cw.updated_at AS created_at,
           ''::text AS file_path
    FROM context_window_topics cwt
    JOIN context_windows cw ON cw.id = cwt.window_id
    JOIN topics t ON t.slug = cwt.topic_slug
    JOIN videos v ON v.id = cw.video_id
    WHERE NOT cw.stale AND cw.kind = 'window'
      AND (sqlc.arg(source_filter)::text = '' OR sqlc.arg(source_filter)::text = 'all' OR sqlc.arg(source_filter)::text = 'context')
      AND sqlc.arg(query)::text <> ''
      AND (t.slug = sqlc.arg(query)
           OR t.title ILIKE '%' || sqlc.arg(query) || '%'
           OR cw.title ILIKE '%' || sqlc.arg(query) || '%'
           OR v.title ILIKE '%' || sqlc.arg(query) || '%')

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
    WHERE sj.created_by = sqlc.arg(owner_id)::uuid AND sj.status = 'ready' AND sj.file_path != ''
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

-- name: GetStitchExportStats :one
SELECT
    COUNT(*) FILTER (WHERE status = 'queued') AS queued_count,
    COUNT(*) FILTER (WHERE status = 'processing') AS processing_count,
    COUNT(*) FILTER (WHERE status = 'ready') AS ready_count,
    COUNT(*) FILTER (WHERE status = 'error') AS error_count,
    COALESCE(SUM(size_bytes) FILTER (WHERE status = 'ready'), 0)::bigint AS total_size_bytes
FROM stitch_jobs
WHERE COALESCE(render_kind, 'export') = 'export';

-- name: ListStitchExportsForAdmin :many
SELECT
    id,
    project_id,
    title,
    status,
    format,
    quality,
    COALESCE(render_kind, 'export') AS render_kind,
    file_path,
    size_bytes,
    progress_pct,
    attempts,
    last_error,
    created_at
FROM stitch_jobs
WHERE COALESCE(render_kind, 'export') = 'export'
ORDER BY created_at DESC
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);

-- name: CountStitchExports :one
SELECT COUNT(*) FROM stitch_jobs WHERE COALESCE(render_kind, 'export') = 'export';

-- name: ListStitchExportFilesByStatus :many
SELECT id, file_path FROM stitch_jobs
WHERE COALESCE(render_kind, 'export') = 'export'
  AND status = sqlc.arg(status);

-- name: DeleteStitchJob :exec
DELETE FROM stitch_jobs WHERE id = sqlc.arg(id);

-- name: DeleteAllStitchExports :exec
DELETE FROM stitch_jobs WHERE COALESCE(render_kind, 'export') = 'export';

-- name: DeleteStitchExportsByStatus :exec
DELETE FROM stitch_jobs
WHERE COALESCE(render_kind, 'export') = 'export'
  AND status = sqlc.arg(status);

-- name: RequeueStitchJob :exec
UPDATE stitch_jobs
SET status = 'queued',
    file_path = '',
    size_bytes = 0,
    locked_at = NULL,
    locked_by = NULL,
    progress_pct = 0,
    started_at = NULL,
    finished_at = NULL,
    last_error = 'Requeued by admin',
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- name: RequeueAllErrorStitchExports :exec
UPDATE stitch_jobs
SET status = 'queued',
    locked_at = NULL,
    locked_by = NULL,
    progress_pct = 0,
    last_error = 'Requeued by admin',
    updated_at = NOW()
WHERE COALESCE(render_kind, 'export') = 'export'
  AND status = 'error';

