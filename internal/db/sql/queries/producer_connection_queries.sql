-- ============================================================================
-- Producer connections (live WebRTC presence + director election)
-- ============================================================================

-- name: UpsertProducerConnection :one
-- Register or refresh a host's live connection.
INSERT INTO producer_connections (show_note_id, user_id)
VALUES (sqlc.arg(show_note_id), sqlc.arg(user_id))
ON CONFLICT (show_note_id, user_id) DO UPDATE SET last_ping = NOW()
RETURNING *;

-- name: GetProducerConnection :one
SELECT * FROM producer_connections
WHERE show_note_id = sqlc.arg(show_note_id) AND user_id = sqlc.arg(user_id);

-- name: ListActiveConnections :many
-- Connections seen within the liveness window, oldest first.
SELECT pc.id, pc.show_note_id, pc.user_id, pc.is_director, pc.connected_at, pc.last_ping, u.user_name AS username
FROM producer_connections pc
JOIN users u ON u.id = pc.user_id
WHERE pc.show_note_id = sqlc.arg(show_note_id)
  AND pc.last_ping > NOW() - INTERVAL '40 seconds'
ORDER BY pc.connected_at ASC;

-- name: GetDirector :one
SELECT * FROM producer_connections
WHERE show_note_id = sqlc.arg(show_note_id)
  AND is_director = TRUE
  AND last_ping > NOW() - INTERVAL '40 seconds'
LIMIT 1;

-- name: ClearDirector :exec
UPDATE producer_connections
SET is_director = FALSE
WHERE show_note_id = sqlc.arg(show_note_id);

-- name: SetDirector :exec
UPDATE producer_connections
SET is_director = TRUE
WHERE show_note_id = sqlc.arg(show_note_id) AND user_id = sqlc.arg(user_id);

-- name: UpdateConnectionPing :exec
UPDATE producer_connections
SET last_ping = NOW()
WHERE show_note_id = sqlc.arg(show_note_id) AND user_id = sqlc.arg(user_id);

-- name: DeleteProducerConnection :exec
DELETE FROM producer_connections
WHERE show_note_id = sqlc.arg(show_note_id) AND user_id = sqlc.arg(user_id);

-- name: DeleteStaleConnections :exec
DELETE FROM producer_connections
WHERE last_ping < NOW() - INTERVAL '2 minutes';
