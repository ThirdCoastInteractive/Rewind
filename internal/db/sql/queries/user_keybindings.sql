-- name: GetUserKeybindings :many
SELECT action, key
FROM user_keybindings
WHERE user_id = $1;

-- name: UpsertUserKeybinding :exec
INSERT INTO user_keybindings (user_id, action, key)
VALUES ($1, $2, $3)
ON CONFLICT (user_id, action)
DO UPDATE SET key = EXCLUDED.key;

-- name: DeleteUserKeybinding :exec
DELETE FROM user_keybindings
WHERE user_id = $1 AND action = $2;

-- name: ResetUserKeybindings :exec
DELETE FROM user_keybindings
WHERE user_id = $1;
