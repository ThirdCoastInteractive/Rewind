-- CreateContextWindowSet inserts a generation set. The unique key is
-- (video_id, transcript_hash, model_digest, prompt_version); a repeat
-- returns the existing row.
-- name: CreateContextWindowSet :one
INSERT INTO context_window_sets (
    video_id,
    transcript_hash,
    model_digest,
    prompt_version,
    status,
    metrics
)
VALUES (
    sqlc.arg(video_id),
    sqlc.arg(transcript_hash),
    sqlc.arg(model_digest),
    sqlc.arg(prompt_version),
    sqlc.arg(status),
    COALESCE(sqlc.arg(metrics)::jsonb, '{}'::jsonb)
)
ON CONFLICT (video_id, transcript_hash, model_digest, prompt_version)
DO UPDATE SET
    status = CASE WHEN context_window_sets.status = 'succeeded' THEN context_window_sets.status ELSE EXCLUDED.status END,
    metrics = CASE WHEN context_window_sets.status = 'succeeded' THEN context_window_sets.metrics ELSE EXCLUDED.metrics END,
    updated_at = NOW()
RETURNING *;

-- name: GetContextWindowSet :one
SELECT * FROM context_window_sets WHERE id = sqlc.arg(id);

-- name: GetContextWindowSetByKey :one
SELECT * FROM context_window_sets
WHERE video_id = sqlc.arg(video_id)
  AND transcript_hash = sqlc.arg(transcript_hash)
  AND model_digest = sqlc.arg(model_digest)
  AND prompt_version = sqlc.arg(prompt_version);

-- name: ListContextWindowSetsForVideo :many
SELECT * FROM context_window_sets
WHERE video_id = sqlc.arg(video_id)
ORDER BY created_at DESC;

-- name: UpdateContextWindowSet :exec
UPDATE context_window_sets
SET status = sqlc.arg(status),
    metrics = COALESCE(sqlc.arg(metrics)::jsonb, metrics),
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- ListContextWindowsByVideo is the raw row set used by reconcile/override copy.
-- name: ListContextWindowsByVideo :many
SELECT * FROM context_windows
WHERE video_id = sqlc.arg(video_id)
  AND stale = FALSE
ORDER BY start_ts, ordinal, created_at;

-- InsertGeneratedContextWindow writes one origin=generated window for a set.
-- created_by is the video's archived_by (users FK).
-- name: InsertGeneratedContextWindow :one
INSERT INTO context_windows (
    video_id, start_ts, end_ts, title, summary, topics, entities, search,
    origin, source_query, transcript_cue_evidence, transcript_version,
    boundary_quality, created_by,
    set_id, ordinal, cue_start, cue_end,
    generated_start_ts, generated_end_ts, confidence,
    override_title, override_summary, override_bounds, stale,
    kind, parent_id, hook
)
SELECT
    sqlc.arg(video_id), sqlc.arg(start_ts), sqlc.arg(end_ts), sqlc.arg(title),
    sqlc.arg(summary), sqlc.arg(topics), sqlc.arg(entities),
    context_window_search_vector(sqlc.arg(title), sqlc.arg(summary), sqlc.arg(topics), sqlc.arg(entities)),
    'generated', sqlc.arg(source_query), sqlc.arg(transcript_cue_evidence),
    v.transcript_version, sqlc.arg(boundary_quality), v.archived_by,
    sqlc.arg(set_id), sqlc.arg(ordinal), sqlc.arg(cue_start), sqlc.arg(cue_end),
    sqlc.narg(generated_start_ts), sqlc.narg(generated_end_ts), sqlc.narg(confidence),
    sqlc.arg(override_title), sqlc.arg(override_summary), sqlc.arg(override_bounds), FALSE,
    sqlc.arg(kind), sqlc.narg(parent_id), sqlc.arg(hook)
FROM videos v WHERE v.id = sqlc.arg(video_id)
ON CONFLICT (set_id,ordinal) DO UPDATE SET start_ts=EXCLUDED.start_ts,end_ts=EXCLUDED.end_ts,title=EXCLUDED.title,summary=EXCLUDED.summary,
 topics=EXCLUDED.topics,entities=EXCLUDED.entities,search=EXCLUDED.search,transcript_version=EXCLUDED.transcript_version,
 cue_start=EXCLUDED.cue_start,cue_end=EXCLUDED.cue_end,transcript_cue_evidence=EXCLUDED.transcript_cue_evidence,generated_start_ts=EXCLUDED.generated_start_ts,generated_end_ts=EXCLUDED.generated_end_ts,confidence=EXCLUDED.confidence,boundary_quality=EXCLUDED.boundary_quality,source_query=EXCLUDED.source_query,
 override_title=EXCLUDED.override_title,override_summary=EXCLUDED.override_summary,override_bounds=EXCLUDED.override_bounds,stale=false,updated_at=now(),
 kind=EXCLUDED.kind,parent_id=EXCLUDED.parent_id,hook=EXCLUDED.hook
RETURNING *;

-- MarkGeneratedWindowsStale flags previous generated rows (optionally keeping a set).
-- name: MarkGeneratedWindowsStale :exec
UPDATE context_windows
SET stale = TRUE,
    updated_at = NOW()
WHERE video_id = sqlc.arg(video_id)
  AND origin = 'generated'
  AND stale = FALSE
  AND (sqlc.narg(except_set_id)::uuid IS NULL OR set_id IS DISTINCT FROM sqlc.narg(except_set_id));

-- MarkOverrideWindowsStale is used when transcript_hash changed: keep the rows,
-- do not delete, but stop treating them as current.
-- name: MarkOverrideWindowsStale :exec
UPDATE context_windows
SET stale = TRUE,
    updated_at = NOW()
WHERE video_id = sqlc.arg(video_id)
  AND stale = FALSE
  AND (override_title OR override_summary OR override_bounds);

-- UpdateContextWindowBounds snaps a generated window; skipped when override_bounds.
-- name: UpdateContextWindowBounds :exec
UPDATE context_windows
SET start_ts = sqlc.arg(start_ts),
    end_ts = sqlc.arg(end_ts),
    cue_start = sqlc.arg(cue_start),
    cue_end = sqlc.arg(cue_end),
    updated_at = NOW()
WHERE id = sqlc.arg(id)
  AND override_bounds = FALSE
  AND stale = FALSE;
