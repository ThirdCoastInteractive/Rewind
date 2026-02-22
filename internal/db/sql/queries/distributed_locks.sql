-- name: TryAdvisoryLock :one
-- Attempts to acquire a PostgreSQL advisory lock (non-blocking)
-- Returns true if the lock was acquired, false if it's already held
SELECT pg_try_advisory_lock(sqlc.arg(lock_id)::bigint) AS acquired;

-- name: AdvisoryUnlock :one
-- Releases a PostgreSQL advisory lock
-- Returns true if the lock was released, false if it wasn't held
SELECT pg_advisory_unlock(sqlc.arg(lock_id)::bigint) AS unlocked;
