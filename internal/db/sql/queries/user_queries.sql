-- insertUser inserts a user into the database
-- it is intentionally kept private, user creation should be done via
-- the NewUser helper found in internal/db/user.go
-- name: insertUser :one
INSERT INTO users (
    id,
    email,
    password,
    user_name,
    role,
    created_at,
    updated_at,
    deleted_at
)
VALUES (
    sqlc.arg(id),
    sqlc.arg(email),
    sqlc.arg(password),
    sqlc.arg(user_name),
    sqlc.arg(role),
    NOW(),
    NOW(),
    NULL
)
RETURNING *;

-- SelectUserByEmail selects a user by email from the database
-- name: SelectUserByEmail :one
SELECT * FROM users WHERE email = sqlc.arg(email) AND deleted_at IS NULL;

-- SelectUserByID selects a user by ID from the database
-- name: SelectUserByID :one
SELECT * FROM users WHERE id = sqlc.arg(id) AND deleted_at IS NULL;

-- SelectUserByUserName selects a user by user name from the database
-- name: SelectUserByUserName :one
SELECT * FROM users WHERE user_name = sqlc.arg(user_name) AND deleted_at IS NULL;

-- DeleteUser soft deletes a user from the database
-- name: DeleteUser :exec
UPDATE users SET deleted_at = NOW() WHERE id = sqlc.arg(id) AND deleted_at IS NULL;

-- ListAllUsers lists all users in the database
-- name: ListAllUsers :many
SELECT * FROM users WHERE deleted_at IS NULL;

-- CountUsers counts non-deleted users
-- name: CountUsers :one
SELECT COUNT(*)::bigint FROM users WHERE deleted_at IS NULL;

-- CountEnabledAdmins counts enabled admin users
-- name: CountEnabledAdmins :one
SELECT COUNT(*)::bigint FROM users WHERE deleted_at IS NULL AND enabled = TRUE AND role = 'admin';

-- SetUserEnabled updates a user's enabled flag
-- name: SetUserEnabled :exec
UPDATE users
SET enabled = sqlc.arg(enabled),
    updated_at = NOW()
WHERE id = sqlc.arg(id) AND deleted_at IS NULL;

-- SetUserRole updates a user's role
-- name: SetUserRole :exec
UPDATE users
SET role = sqlc.arg(role),
    updated_at = NOW()
WHERE id = sqlc.arg(id) AND deleted_at IS NULL;

-- UsernameTaken checks if a username is already taken 
-- name: UsernameTaken :one
SELECT EXISTS (
    SELECT 1
    FROM users
    WHERE user_name = sqlc.arg(user_name) AND deleted_at IS NULL
);

-- EmailRegistered checks if an email is already registered
-- name: EmailRegistered :one
SELECT EXISTS (
    SELECT 1
    FROM users
    WHERE email = sqlc.arg(email) AND deleted_at IS NULL
);

-- UpdateUser updates a user in the database
-- name: UpdateUser :exec
UPDATE users
SET email = sqlc.arg(email),
    password = sqlc.arg(password),
    user_name = sqlc.arg(user_name),
    updated_at = NOW(),
    email_verified = sqlc.arg(email_verified)
WHERE id = sqlc.arg(id) AND deleted_at IS NULL;

-- InvalidateUserSessions marks all sessions for a user as invalid.
-- Any session created before this timestamp will be rejected.
-- name: InvalidateUserSessions :exec
UPDATE users
SET sessions_invalidated_at = NOW(),
    updated_at = NOW()
WHERE id = sqlc.arg(id) AND deleted_at IS NULL;

-- GetSessionInvalidation returns the sessions_invalidated_at and enabled
-- flag for a user. Used by middleware to check if a session is still valid.
-- name: GetSessionInvalidation :one
SELECT sessions_invalidated_at, enabled
FROM users
WHERE id = sqlc.arg(id) AND deleted_at IS NULL;
