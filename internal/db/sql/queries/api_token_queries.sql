-- name: InsertAPIToken :one
INSERT INTO api_tokens (user_id, name, token_hash, scopes)
VALUES (sqlc.arg(user_id), sqlc.arg(name), sqlc.arg(token_hash), sqlc.arg(scopes))
RETURNING *;

-- name: GetAPITokenByHash :one
SELECT * FROM api_tokens
WHERE token_hash = sqlc.arg(token_hash)
  AND revoked_at IS NULL;

-- name: ListAPITokensByUser :many
SELECT id, created_at, last_used_at, user_id, name, scopes, revoked_at
FROM api_tokens
WHERE user_id = sqlc.arg(user_id)
ORDER BY created_at DESC;

-- name: RevokeAPIToken :exec
UPDATE api_tokens
SET revoked_at = NOW()
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id) AND revoked_at IS NULL;

-- name: TouchAPIToken :exec
UPDATE api_tokens SET last_used_at = NOW() WHERE id = sqlc.arg(id);
