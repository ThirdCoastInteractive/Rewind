-- name: ListClipsByVideo :many
SELECT * FROM clips
WHERE video_id = sqlc.arg(video_id)
ORDER BY start_ts ASC;

-- name: GetClip :one
SELECT * FROM clips
WHERE id = sqlc.arg(id);

-- name: CreateClip :one
INSERT INTO clips (
    video_id,
    start_ts,
    end_ts,
    duration,
    title,
    description,
    color,
    tags,
    created_by
) VALUES (
    sqlc.arg(video_id),
    sqlc.arg(start_ts),
    sqlc.arg(end_ts),
    sqlc.arg(duration),
    sqlc.arg(title),
    sqlc.arg(description),
    sqlc.arg(color),
    sqlc.arg(tags),
    sqlc.arg(created_by)
) RETURNING *;

-- name: UpdateClip :one
UPDATE clips
SET
    start_ts = COALESCE(sqlc.narg('start_ts'), start_ts),
    end_ts = COALESCE(sqlc.narg('end_ts'), end_ts),
    duration = COALESCE(sqlc.narg('duration'), duration),
    title = COALESCE(sqlc.narg('title'), title),
    description = COALESCE(sqlc.narg('description'), description),
    color = COALESCE(sqlc.narg('color'), color),
    tags = COALESCE(sqlc.narg('tags'), tags),
    filter_stack = COALESCE(sqlc.narg('filter_stack'), filter_stack),
    updated_at = NOW()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: UpdateClipCrops :exec
UPDATE clips
SET crops = sqlc.arg(crops),
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- name: UpdateClipFilterStack :exec
UPDATE clips
SET filter_stack = sqlc.arg(filter_stack),
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- name: DeleteClip :exec
DELETE FROM clips
WHERE id = sqlc.arg(id);

-- name: DeleteClipsByVideo :exec
DELETE FROM clips
WHERE video_id = sqlc.arg(video_id);

-- name: GetClipExportStorageLimit :one
SELECT COALESCE(clip_export_storage_limit_bytes, 0) FROM instance_settings WHERE id = 1;

-- name: GetTotalClipExportSize :one
SELECT COALESCE(SUM(size_bytes), 0)::bigint FROM clip_exports WHERE status = 'ready';

-- name: GetClipExportStats :one
-- Get export statistics for admin dashboard
SELECT 
    COUNT(*) FILTER (WHERE status = 'queued') AS queued_count,
    COUNT(*) FILTER (WHERE status = 'processing') AS processing_count,
    COUNT(*) FILTER (WHERE status = 'ready') AS ready_count,
    COUNT(*) FILTER (WHERE status = 'error') AS error_count,
    COALESCE(SUM(size_bytes) FILTER (WHERE status = 'ready'), 0)::bigint AS total_size_bytes
FROM clip_exports;

-- name: ListClipExportsForAdmin :many
-- List exports with clip/video info for admin management
SELECT 
    ce.id,
    ce.clip_id,
    c.video_id,
    ce.status,
    ce.variant,
    ce.file_path,
    ce.size_bytes,
    ce.progress_pct,
    ce.attempts,
    ce.last_error,
    ce.created_at,
    ce.updated_at,
    c.title AS clip_label,
    c.duration AS clip_duration,
    v.title AS video_title
FROM clip_exports ce
JOIN clips c ON c.id = ce.clip_id
JOIN videos v ON v.id = c.video_id
ORDER BY ce.created_at DESC
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);

-- name: CountClipExports :one
SELECT COUNT(*) FROM clip_exports;

-- name: DeleteAllClipExports :exec
-- Delete all exports (files must be cleaned up separately)
DELETE FROM clip_exports;

-- name: DeleteClipExportsByStatus :exec
-- Delete exports by status (files must be cleaned up separately)
DELETE FROM clip_exports WHERE status = sqlc.arg(status);

-- name: ListClipExportFilesByStatus :many
-- Get file paths for exports by status (for cleanup before delete)
SELECT id, file_path FROM clip_exports 
WHERE status = sqlc.arg(status) AND file_path != '';

-- name: RequeueAllErrorExports :exec
-- Requeue all failed exports
UPDATE clip_exports
SET status = 'queued',
    locked_at = NULL,
    locked_by = NULL,
    pid = NULL,
    progress_pct = 0,
    attempts = 0,
    last_error = NULL,
    updated_at = NOW()
WHERE status = 'error';

-- name: ListOldestClipExportsForCleanup :many
SELECT id, file_path, size_bytes FROM clip_exports
WHERE status = 'ready'
ORDER BY last_accessed_at ASC NULLS FIRST;

-- name: DeleteClipExport :exec
DELETE FROM clip_exports WHERE id = sqlc.arg(id);

-- name: FindReusableClipExport :one
SELECT id, file_path 
FROM clip_exports
WHERE clip_exports.clip_id = sqlc.arg(clip_id)
  AND clip_exports.created_by = sqlc.arg(created_by)
  AND clip_exports.format = sqlc.arg(format)
  AND clip_exports.variant = sqlc.arg(variant)
  AND clip_exports.status = 'ready'
  AND clip_exports.clip_updated_at >= (SELECT clips.updated_at FROM clips WHERE clips.id = sqlc.arg(clip_id))
ORDER BY clip_exports.created_at DESC
LIMIT 1;

-- name: UpdateClipExportLastAccessed :exec
UPDATE clip_exports 
SET last_accessed_at = NOW(), updated_at = NOW() 
WHERE id = sqlc.arg(id);

-- name: CreateClipExport :one
INSERT INTO clip_exports (clip_id, created_by, format, variant, spec, clip_updated_at, file_path, status, created_at, updated_at)
VALUES (sqlc.arg(clip_id), sqlc.arg(created_by), sqlc.arg(format), sqlc.arg(variant), sqlc.arg(spec), sqlc.arg(clip_updated_at), '', 'queued', NOW(), NOW())
RETURNING id;

-- name: UpdateClipExportFilePath :exec
UPDATE clip_exports 
SET file_path = sqlc.arg(file_path), updated_at = NOW() 
WHERE id = sqlc.arg(id);

-- name: GetClipExportByID :one
SELECT id, file_path, status, last_error
FROM clip_exports
WHERE id = sqlc.arg(id);

-- name: GetClipExportForDownload :one
SELECT ce.file_path, ce.format, ce.status, ce.clip_id, ce.variant,
       COALESCE(c.title, '') AS clip_title,
       c.crops
FROM clip_exports ce
JOIN clips c ON c.id = ce.clip_id
WHERE ce.id = sqlc.arg(id);

-- ============================================================================
-- ENCODER WORKER QUERIES
-- ============================================================================

-- name: FindAndLockPendingClipExport :one
-- Atomically find the oldest queued export and lock it for processing.
-- Uses FOR UPDATE SKIP LOCKED so concurrent workers never claim the same row.
UPDATE clip_exports
SET status = 'processing',
    locked_at = NOW(),
    locked_by = sqlc.arg(locked_by),
    started_at = NOW(),
    attempts = attempts + 1,
    updated_at = NOW()
WHERE id = (
    SELECT id FROM clip_exports
    WHERE status = 'queued'
      AND (locked_at IS NULL OR locked_at < NOW() - INTERVAL '10 minutes')
    ORDER BY created_at ASC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING id, clip_id, created_by, format, variant, spec, clip_updated_at;

-- name: UpdateClipExportPID :exec
-- Store the ffmpeg process PID for potential cleanup
UPDATE clip_exports
SET pid = sqlc.arg(pid),
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- name: ClearClipExportPID :exec
-- Clear the PID when process completes
UPDATE clip_exports
SET pid = NULL,
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- name: FindOrphanedExportsWithPID :many
-- Find processing exports with PIDs that belong to this worker (for cleanup on restart)
SELECT id, pid, file_path
FROM clip_exports
WHERE status = 'processing'
  AND locked_by = sqlc.arg(locked_by)
  AND pid IS NOT NULL;

-- name: UnlockClipExport :exec
-- Release lock without changing status (for graceful shutdown)
UPDATE clip_exports
SET locked_at = NULL,
    locked_by = NULL,
    pid = NULL,
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- name: UpdateClipExportProgress :exec
-- Update progress percentage during encoding
UPDATE clip_exports
SET progress_pct = sqlc.arg(progress_pct),
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- name: FinishClipExportReady :exec
-- Mark export as ready with file info
UPDATE clip_exports
SET status = 'ready',
    file_path = sqlc.arg(file_path),
    size_bytes = sqlc.arg(size_bytes),
    progress_pct = 100,
    finished_at = NOW(),
    locked_at = NULL,
    locked_by = NULL,
    pid = NULL,
    last_accessed_at = NOW(),
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- name: FinishClipExportError :exec
-- Mark export as failed with error message
UPDATE clip_exports
SET status = 'error',
    last_error = sqlc.arg(last_error),
    finished_at = NOW(),
    locked_at = NULL,
    locked_by = NULL,
    pid = NULL,
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- name: GetClipExportStatus :one
-- Get current export status for SSE streaming
SELECT id, clip_id, status, progress_pct, file_path, last_error
FROM clip_exports
WHERE id = sqlc.arg(id);

-- name: FindOrCreatePendingClipExport :one
-- Find existing queued/processing export that is NOT stuck (updated in last 5 minutes)
SELECT id, status, progress_pct, file_path
FROM clip_exports
WHERE clip_id = sqlc.arg(clip_id)
  AND created_by = sqlc.arg(created_by)
  AND format = sqlc.arg(format)
  AND variant = sqlc.arg(variant)
  AND status IN ('queued', 'processing')
  AND updated_at > NOW() - INTERVAL '5 minutes'
ORDER BY created_at DESC
LIMIT 1;

-- name: ResetStuckExports :exec
-- Reset stuck exports that have been in processing state too long without updates
UPDATE clip_exports
SET status = 'queued',
    locked_at = NULL,
    locked_by = NULL,
    progress_pct = 0,
    updated_at = NOW()
WHERE status = 'processing'
  AND updated_at < NOW() - INTERVAL '5 minutes';

-- name: RequeueClipExport :exec
-- Re-queue an export that was marked ready but file is missing
UPDATE clip_exports
SET status = 'queued',
    file_path = '',
    size_bytes = 0,
    locked_at = NULL,
    locked_by = NULL,
    progress_pct = 0,
    started_at = NULL,
    finished_at = NULL,
    last_error = 'Requeued: output file was missing',
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- name: FindReadyExportsWithMissingFiles :many
-- Find ready exports to check for missing files (for cleanup/requeue on startup)
SELECT id, file_path, clip_id
FROM clip_exports
WHERE status = 'ready'
  AND file_path != ''
ORDER BY created_at DESC
LIMIT 500;

-- name: ListActiveExportsForClips :many
-- Get active exports for a list of clip IDs (for clip bank hydration)
-- Only show processing/queued exports that are actively being worked on (updated in last 5 min)
SELECT id, clip_id, status, progress_pct, file_path
FROM clip_exports
WHERE clip_id = ANY(sqlc.arg(clip_ids)::uuid[])
  AND (
    (status IN ('queued', 'processing') AND updated_at > NOW() - INTERVAL '5 minutes')
    OR status = 'ready'
  )
ORDER BY clip_id, created_at DESC;

-- name: GetClipForExport :one
-- Get clip data needed for encoding
SELECT c.id, c.video_id, c.start_ts, c.end_ts, c.duration, c.crops, c.filter_stack,
       c.title AS clip_title, v.video_path
FROM clips c
JOIN videos v ON v.id = c.video_id
WHERE c.id = sqlc.arg(id);
