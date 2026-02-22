-- InsertCookie inserts a new cookie for a user
-- name: InsertCookie :exec
INSERT INTO cookies (user_id, domain, flag, path, secure, expiration, name, value)
VALUES (
    sqlc.arg(user_id),
    sqlc.arg(domain),
    sqlc.arg(flag),
    sqlc.arg(path),
    sqlc.arg(secure),
    sqlc.arg(expiration),
    sqlc.arg(name),
    sqlc.arg(value)
)
ON CONFLICT (user_id, domain, name, path)
DO UPDATE SET
    flag = EXCLUDED.flag,
    secure = EXCLUDED.secure,
    expiration = EXCLUDED.expiration,
    value = EXCLUDED.value,
    updated_at = NOW();

-- GetUserCookies returns all cookies for a user in Netscape format
-- name: GetUserCookies :many
SELECT domain, flag, path, secure, expiration, name, value
FROM cookies
WHERE user_id = sqlc.arg(user_id)
ORDER BY domain, name, path;

-- DeleteUserCookies deletes all cookies for a user
-- name: DeleteUserCookies :exec
DELETE FROM cookies
WHERE user_id = sqlc.arg(user_id);

-- CountUserCookies counts the number of cookies for a user
-- name: CountUserCookies :one
SELECT COUNT(*) as count
FROM cookies
WHERE user_id = sqlc.arg(user_id);
