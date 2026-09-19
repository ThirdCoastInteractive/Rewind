-- name: CreateCompilationPlan :one
INSERT INTO compilation_plans (created_by, creator_id, source_query, title)
VALUES (sqlc.arg(created_by), sqlc.narg(creator_id), sqlc.arg(source_query), sqlc.arg(title))
RETURNING *;

-- name: SetInitialCompilationPlanDuration :one
UPDATE compilation_plans SET estimated_duration = sqlc.arg(estimated_duration), updated_at = NOW()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: GetCompilationPlan :one
SELECT * FROM compilation_plans WHERE id = sqlc.arg(id);

-- name: ListCompilationPlansForUser :many
SELECT * FROM compilation_plans
WHERE created_by = sqlc.arg(created_by)
ORDER BY updated_at DESC
LIMIT sqlc.arg(page_limit);

-- name: ListCompilationPlanSegments :many
SELECT cps.*, v.title AS video_title, v.uploader, v.media, v.src, v.upload_date, v.duration_seconds
FROM compilation_plan_segments cps JOIN videos v ON v.id = cps.video_id
WHERE cps.plan_id = sqlc.arg(plan_id)
ORDER BY cps.position;

-- name: ClearCompilationPlanSegments :exec
DELETE FROM compilation_plan_segments WHERE plan_id = sqlc.arg(plan_id);

-- name: AddCompilationPlanSegment :one
INSERT INTO compilation_plan_segments (
    plan_id, position, video_id, start_ts, end_ts, context_window_id,
    match_evidence, selection_rationale, media_ready
)
SELECT sqlc.arg(plan_id), sqlc.arg(position), sqlc.arg(video_id), sqlc.arg(start_ts), sqlc.arg(end_ts),
       sqlc.narg(context_window_id), sqlc.arg(match_evidence), sqlc.arg(selection_rationale),
       (v.media <> 'metadata' AND v.video_path IS NOT NULL)
FROM videos v WHERE v.id = sqlc.arg(video_id)
RETURNING *;

-- name: BumpCompilationPlanRevision :one
UPDATE compilation_plans SET revision = revision + 1, status = 'draft', stitch_job_id = NULL,
    estimated_duration = sqlc.arg(estimated_duration), last_error = '', updated_at = NOW()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: SetCompilationPlanState :exec
UPDATE compilation_plans SET status = sqlc.arg(status),
    stitch_project_id = COALESCE(sqlc.narg(stitch_project_id), stitch_project_id),
    stitch_job_id = COALESCE(sqlc.narg(stitch_job_id), stitch_job_id),
    last_error = sqlc.arg(last_error), updated_at = NOW()
WHERE id = sqlc.arg(id);

-- name: SetCompilationSegmentDownload :exec
UPDATE compilation_plan_segments SET download_job_id = sqlc.arg(download_job_id),
    failure_state = '', updated_at = NOW()
WHERE id = sqlc.arg(id) AND download_job_id IS NULL;

-- name: SetCompilationSegmentFailure :exec
UPDATE compilation_plan_segments SET failure_state = sqlc.arg(failure_state), updated_at = NOW()
WHERE id = sqlc.arg(id);

-- name: NotifyStitchJob :exec
SELECT pg_notify('stitch_jobs', sqlc.arg(payload));
