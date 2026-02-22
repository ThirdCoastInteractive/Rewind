-- name: ListMarkersByVideo :many
SELECT * FROM markers
WHERE video_id = sqlc.arg(video_id)
ORDER BY timestamp ASC;

-- name: GetMarker :one
SELECT * FROM markers
WHERE id = sqlc.arg(id);

-- name: CreateMarker :one
INSERT INTO markers (
    video_id,
    timestamp,
    title,
    description,
    color,
    marker_type,
    duration,
    created_by
) VALUES (
    sqlc.arg(video_id),
    sqlc.arg(timestamp),
    sqlc.arg(title),
    sqlc.arg(description),
    sqlc.arg(color),
    sqlc.arg(marker_type),
    sqlc.arg(duration),
    sqlc.arg(created_by)
) RETURNING *;

-- name: UpdateMarker :one
UPDATE markers
SET
    timestamp = COALESCE(sqlc.narg('timestamp'), timestamp),
    title = COALESCE(sqlc.narg('title'), title),
    description = COALESCE(sqlc.narg('description'), description),
    color = COALESCE(sqlc.narg('color'), color),
    marker_type = COALESCE(sqlc.narg('marker_type'), marker_type),
    duration = COALESCE(sqlc.narg('duration'), duration)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteMarker :exec
DELETE FROM markers
WHERE id = sqlc.arg(id);

-- name: DeleteMarkersByVideo :exec
DELETE FROM markers
WHERE video_id = sqlc.arg(video_id);
