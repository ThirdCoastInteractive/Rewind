-- name: CreatePlayerSession :one
INSERT INTO player_sessions (producer_id, session_code, expires_at)
VALUES (sqlc.arg(producer_id), sqlc.arg(session_code), sqlc.arg(expires_at))
RETURNING *;

-- name: GetPlayerSessionByCode :one
SELECT * FROM player_sessions
WHERE session_code = sqlc.arg(session_code) AND expires_at > NOW();

-- name: GetPlayerSessionByID :one
SELECT * FROM player_sessions
WHERE id = sqlc.arg(id);

-- name: GetActiveSessionByProducer :one
SELECT * FROM player_sessions
WHERE producer_id = sqlc.arg(producer_id) AND expires_at > NOW()
ORDER BY created_at DESC
LIMIT 1;

-- name: ListSessionsByProducer :many
SELECT * FROM player_sessions
WHERE producer_id = sqlc.arg(producer_id)
ORDER BY created_at DESC;

-- name: UpdatePlayerSessionActivity :exec
UPDATE player_sessions
SET last_activity = NOW()
WHERE id = sqlc.arg(id);

-- name: UpdatePlayerSessionState :exec
UPDATE player_sessions
SET state = sqlc.arg(state), last_activity = NOW()
WHERE id = sqlc.arg(id);

-- name: UpdatePlayerSessionVideo :exec
UPDATE player_sessions
SET current_video_id = sqlc.arg(video_id), last_activity = NOW()
WHERE id = sqlc.arg(id);

-- name: ClearVideoFromPlayerSessions :exec
UPDATE player_sessions
SET current_video_id = NULL, last_activity = NOW()
WHERE current_video_id = sqlc.arg(video_id);

-- name: DeletePlayerSession :exec
DELETE FROM player_sessions
WHERE id = sqlc.arg(id);

