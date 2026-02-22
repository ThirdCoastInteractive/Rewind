-- name: CreateExtensionToken :one
INSERT INTO extension_tokens (user_id, token, expires_at)
VALUES (sqlc.arg(user_id), sqlc.arg(token), sqlc.arg(expires_at))
RETURNING *;

-- name: GetExtensionTokenByToken :one
SELECT * FROM extension_tokens
WHERE token = sqlc.arg(token) AND NOT revoked AND expires_at > NOW();

-- name: UpdateExtensionTokenLastUsed :exec
UPDATE extension_tokens
SET last_used_at = NOW()
WHERE token = sqlc.arg(token);

-- name: RevokeExtensionToken :exec
UPDATE extension_tokens
SET revoked = TRUE
WHERE token = sqlc.arg(token);

-- name: RevokeAllUserExtensionTokens :exec
UPDATE extension_tokens
SET revoked = TRUE
WHERE user_id = sqlc.arg(user_id);

