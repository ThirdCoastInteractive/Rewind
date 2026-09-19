-- name: LockReviewThread :one
SELECT * FROM show_note_review_threads WHERE id=sqlc.arg(id) FOR UPDATE;

-- name: QueueNoteMaterialization :exec
INSERT INTO show_note_materializations(thread_id,show_note_id,user_id,accepted_revision,base_markdown,proposed_markdown)
VALUES(sqlc.arg(thread_id),sqlc.arg(show_note_id),sqlc.arg(user_id),sqlc.arg(accepted_revision),sqlc.arg(base_markdown),sqlc.arg(proposed_markdown)) ON CONFLICT DO NOTHING;

-- name: ClaimNoteMaterialization :one
UPDATE show_note_materializations SET lease_token=gen_random_uuid(),retry_at=now()+interval '2 minutes',attempts=attempts+1
WHERE thread_id=(SELECT thread_id FROM show_note_materializations WHERE status='pending' AND retry_at<=now() ORDER BY retry_at LIMIT 1 FOR UPDATE SKIP LOCKED) RETURNING *;

-- name: FinishNoteMaterialization :exec
UPDATE show_note_materializations SET status=sqlc.arg(status),last_error=sqlc.arg(last_error),retry_at=now()+interval '30 seconds',lease_token=NULL
WHERE thread_id=sqlc.arg(thread_id) AND lease_token=sqlc.arg(lease_token);
