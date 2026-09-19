-- name: LockDelegatedMessage :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(identity)::text,0));
-- name: GetDelegatedMessage :one
SELECT * FROM agent_delegated_messages WHERE user_id=$1 AND message_id=$2;
-- name: SaveDelegatedMessage :exec
INSERT INTO agent_delegated_messages(user_id,message_id,request_hash,run_id) VALUES($1,$2,$3,$4);
