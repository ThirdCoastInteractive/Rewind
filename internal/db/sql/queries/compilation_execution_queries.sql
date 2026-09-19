-- name: LockCompilationPlan :one
SELECT * FROM compilation_plans WHERE id=sqlc.arg(id) FOR UPDATE;

-- name: CreateCompilationExecution :one
INSERT INTO compilation_executions(plan_id,revision,created_by,title)
SELECT p.id,p.revision,p.created_by,p.title FROM compilation_plans p WHERE p.id=sqlc.arg(plan_id)
ON CONFLICT(plan_id,revision) DO UPDATE SET plan_id=EXCLUDED.plan_id RETURNING *;

-- name: SnapshotCompilationSegments :exec
INSERT INTO compilation_execution_segments(execution_id,position,video_id,start_ts,end_ts,evidence,rationale)
SELECT sqlc.arg(execution_id),position,video_id,start_ts,end_ts,match_evidence,selection_rationale FROM compilation_plan_segments WHERE plan_id=sqlc.arg(plan_id)
ON CONFLICT DO NOTHING;

-- name: ListCompilationExecutions :many
SELECT * FROM compilation_executions WHERE plan_id=sqlc.arg(plan_id) ORDER BY revision DESC;

-- name: HasClaimableCompilation :one
SELECT EXISTS(
    SELECT 1 FROM compilation_executions
    WHERE status IN ('waiting_media','rendering') AND next_check<=now()
)::boolean;

-- name: ClaimCompilationExecution :one
SELECT * FROM compilation_executions WHERE status IN ('waiting_media','rendering') AND next_check<=now() ORDER BY next_check,id LIMIT 1 FOR UPDATE SKIP LOCKED;

-- WakePendingCompilations makes waiting executions claimable after a download,
-- stitch, or compilation notification. Idle rows still use next_check backoff
-- so a single wake cannot spin the same execution.
-- name: WakePendingCompilations :exec
UPDATE compilation_executions SET next_check=now() WHERE status IN ('waiting_media','rendering');

-- name: ListExecutionSegments :many
SELECT s.*,v.title,v.src,v.media,v.video_path,d.status AS download_status,d.last_error AS download_error
FROM compilation_execution_segments s JOIN videos v ON v.id=s.video_id LEFT JOIN download_jobs d ON d.id=s.download_job_id
WHERE execution_id=sqlc.arg(execution_id) ORDER BY position;

-- name: LinkExecutionDownload :exec
UPDATE compilation_execution_segments SET download_job_id=sqlc.arg(download_job_id) WHERE execution_id=sqlc.arg(execution_id) AND position=sqlc.arg(position);

-- name: UpdateCompilationExecution :exec
UPDATE compilation_executions SET status=sqlc.arg(status),last_error=sqlc.arg(last_error),stitch_job_id=COALESCE(sqlc.narg(stitch_job_id),stitch_job_id),stitch_project_id=COALESCE(sqlc.narg(stitch_project_id),stitch_project_id),next_check=now()+interval '30 seconds',updated_at=now() WHERE id=sqlc.arg(id);

-- name: PublishExecutionState :exec
UPDATE compilation_plans p SET status=e.status,last_error=e.last_error,stitch_project_id=e.stitch_project_id,stitch_job_id=e.stitch_job_id,updated_at=now()
FROM compilation_executions e WHERE e.id=sqlc.arg(execution_id) AND p.id=e.plan_id AND p.revision=e.revision;

-- name: RetryCompilationExecution :exec
UPDATE compilation_executions SET status='waiting_media',last_error='',stitch_job_id=NULL,next_check=now() WHERE id=sqlc.arg(id) AND status='failed';

-- name: ClearFailedExecutionDownloads :exec
UPDATE compilation_execution_segments s SET download_job_id=NULL FROM download_jobs d WHERE s.execution_id=sqlc.arg(execution_id) AND d.id=s.download_job_id AND d.status='failed';
