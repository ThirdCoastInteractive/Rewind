-- name: InsertYtdlpLog :exec
INSERT INTO ytdlp_logs (job_id, stream, message)
VALUES (sqlc.arg(job_id), sqlc.arg(stream), sqlc.arg(message));

-- name: GetYtdlpLogsForJob :many
SELECT id, job_id, stream, message, created_at
FROM ytdlp_logs
WHERE job_id = sqlc.arg(job_id)
ORDER BY created_at ASC, id ASC;

-- name: GetYtdlpLogsForJobSince :many
SELECT id, job_id, stream, message, created_at
FROM ytdlp_logs
WHERE job_id = sqlc.arg(job_id) AND created_at > sqlc.arg(since)
ORDER BY created_at ASC, id ASC;

-- name: GetYtdlpLogsForJobPaginated :many
SELECT id, job_id, stream, message, created_at
FROM ytdlp_logs
WHERE job_id = sqlc.arg(job_id)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountYtdlpLogsForJob :one
SELECT COUNT(*) FROM ytdlp_logs WHERE job_id = sqlc.arg(job_id);
