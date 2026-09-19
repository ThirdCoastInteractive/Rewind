-- EnqueueMLJob inserts a queued ML job. UNIQUE (video_id, kind, transcript_hash,
-- model_digest, prompt_version) makes a duplicate enqueue a no-op.
-- name: EnqueueMLJob :exec
INSERT INTO ml_jobs (
    video_id,
    kind,
    status,
    priority,
    transcript_hash,
    model_digest,
    prompt_version
)
VALUES (
    sqlc.arg(video_id),
    sqlc.arg(kind),
    'queued',
    sqlc.arg(priority),
    sqlc.arg(transcript_hash),
    sqlc.arg(model_digest),
    sqlc.arg(prompt_version)
)
ON CONFLICT (video_id, kind, transcript_hash, model_digest, prompt_version) DO NOTHING;

-- RequeueMLJob flips a terminal row with the same uniqueness key back to queued
-- so caption regeneration is not swallowed by ON CONFLICT DO NOTHING.
-- name: RequeueMLJob :exec
UPDATE ml_jobs
SET status = 'queued',
    failure_count = 0,
    retry_at = now(),
    last_error = '',
    locked_at = NULL,
    locked_by = '',
    updated_at = NOW()
WHERE video_id = sqlc.arg(video_id)
  AND kind = sqlc.arg(kind)
  AND transcript_hash = sqlc.arg(transcript_hash)
  AND status IN ('failed', 'succeeded', 'waiting_model', 'waiting_assets', 'retry_wait', 'superseded', 'paused', 'cancelled');

-- ClaimMLJob takes one queued (or cooled-down waiting_model) row of the
-- requested kinds. Serial per kind so transcribe cannot starve visual_index.
-- waiting_model / waiting_assets reclaims do not increment attempts: those
-- are infrastructure waits, not retries toward a failed-job cap.
-- ORDER BY priority, created_at. FOR UPDATE SKIP LOCKED.
-- name: ClaimMLJob :one
UPDATE ml_jobs
SET status = 'processing',
    locked_at = NOW(),
    locked_by = sqlc.arg(worker_id),
    lease_token = gen_random_uuid(),
    attempts = CASE WHEN status IN ('queued', 'retry_wait') THEN attempts + 1 ELSE attempts END,
    updated_at = NOW()
WHERE id = (
    SELECT j.id
    FROM ml_jobs j
    WHERE j.kind <> 'face_index'
      AND j.kind = ANY(sqlc.arg(kinds)::text[])
      AND j.status IN ('queued', 'waiting_model', 'waiting_assets', 'retry_wait')
      AND j.retry_at <= now()
      AND NOT EXISTS (SELECT 1 FROM ml_runtime_health h WHERE h.kind=j.kind AND h.retry_at>now())
      AND (
          j.status = 'queued'
          OR j.retry_at <= now()
      )
      AND (
          SELECT count(*)
          FROM ml_jobs active
          WHERE active.status = 'processing'
            AND active.kind = j.kind
            AND active.locked_at > NOW() - INTERVAL '15 minutes'
      ) < sqlc.arg(slot_limit)::int
    ORDER BY j.priority, j.created_at
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- FinishMLJob records a terminal or waiting_model status and releases the lock.
-- name: FinishMLJob :exec
UPDATE ml_jobs
SET status = sqlc.arg(status),
    failure_count = CASE WHEN sqlc.arg(status)::text IN ('failed','retry_wait') THEN failure_count+1 WHEN sqlc.arg(status)::text IN ('succeeded','superseded') THEN 0 ELSE failure_count END,
    last_error = sqlc.arg(last_error),
    model_digest = COALESCE(NULLIF(sqlc.arg(model_digest), ''), model_digest),
    prompt_version = COALESCE(NULLIF(sqlc.arg(prompt_version), ''), prompt_version),
    locked_at = NULL,
    locked_by = '',
    retry_at = now() + make_interval(secs => sqlc.arg(retry_seconds)::int),
    updated_at = NOW()
WHERE id = sqlc.arg(id) AND lease_token = sqlc.arg(lease_token) AND status='processing';

-- HeartbeatMLJob refreshes the serial-GPU lock so RecoverStuckMLJobs ignores
-- a long whisper/ollama run.
-- name: HeartbeatMLJob :execrows
UPDATE ml_jobs
SET locked_at = NOW(),
    updated_at = NOW()
WHERE id = sqlc.arg(id)
  AND lease_token = sqlc.arg(lease_token)
  AND status = 'processing';

-- UpdateMLJobProgress stores operator-visible progress and refreshes the lease.
-- name: UpdateMLJobProgress :execrows
UPDATE ml_jobs
SET checkpoint = sqlc.arg(checkpoint),
    locked_at = NOW(),
    updated_at = NOW()
WHERE id = sqlc.arg(id)
  AND lease_token = sqlc.arg(lease_token)
  AND status = 'processing';

-- RecoverStuckMLJobs requeues processing rows whose lock is older than 15m.
-- name: RecoverStuckMLJobs :exec
UPDATE ml_jobs
SET status = 'queued',
    locked_at = NULL,
    locked_by = '',
    updated_at = NOW()
WHERE status = 'processing'
  AND (locked_at IS NULL OR locked_at < NOW() - INTERVAL '15 minutes');

-- GetMLJob returns one job by id.
-- name: GetMLJob :one
SELECT * FROM ml_jobs WHERE id = sqlc.arg(id);

-- ListMLJobsForVideo lists jobs for a video, newest first.
-- name: ListMLJobsForVideo :many
SELECT * FROM ml_jobs
WHERE video_id = sqlc.arg(video_id)
ORDER BY created_at DESC;

-- name: ListRecentMLJobs :many
SELECT j.*, v.title AS video_title
FROM ml_jobs j JOIN videos v ON v.id = j.video_id
WHERE j.status NOT IN ('paused', 'cancelled')
ORDER BY
  CASE j.status
    WHEN 'processing' THEN 0
    WHEN 'queued' THEN 1
    WHEN 'retry_wait' THEN 2
    WHEN 'waiting_model' THEN 3
    WHEN 'waiting_assets' THEN 4
    ELSE 5
  END,
  j.priority,
  j.updated_at DESC
LIMIT 100;

-- name: CountMLJobs :many
SELECT kind, status, count(*)::bigint AS n
FROM ml_jobs
GROUP BY kind, status
ORDER BY kind, status;

-- name: ListMLQueue :many
SELECT j.*, v.title AS video_title
FROM ml_jobs j JOIN videos v ON v.id = j.video_id
WHERE (sqlc.arg(kind) = '' OR j.kind = sqlc.arg(kind))
  AND (sqlc.arg(status) = '' OR j.status = sqlc.arg(status))
ORDER BY
  CASE j.status
    WHEN 'processing' THEN 0
    WHEN 'queued' THEN 1
    WHEN 'retry_wait' THEN 2
    WHEN 'waiting_model' THEN 3
    WHEN 'waiting_assets' THEN 4
    ELSE 5
  END,
  j.priority,
  j.created_at
LIMIT sqlc.arg(row_limit);

-- name: GetMLJobByKey :one
SELECT * FROM ml_jobs
WHERE video_id = sqlc.arg(video_id)
  AND kind = sqlc.arg(kind)
  AND transcript_hash = sqlc.arg(transcript_hash)
ORDER BY updated_at DESC
LIMIT 1;

-- name: ListenMLJobs :exec
LISTEN ml_jobs;

-- PromoteContextJobs puts an explicit/agent request ahead of demand backfill
-- and revives a cancelled row for the same transcript fingerprint.
-- name: PromoteContextJobs :exec
UPDATE ml_jobs
SET priority = LEAST(priority, 100),
    status = CASE WHEN status IN ('paused', 'cancelled') THEN 'queued' ELSE status END,
    retry_at = CASE WHEN status IN ('paused', 'cancelled') THEN now() ELSE retry_at END,
    updated_at = now()
WHERE video_id = sqlc.arg(video_id) AND transcript_hash = sqlc.arg(transcript_hash)
  AND kind = 'context_windows' AND status IN ('queued', 'paused', 'cancelled');

-- SupersedeStaleContextJobs drops queued demand rows whose transcript fingerprint
-- is no longer current so they cannot run ahead of the live hash.
-- name: SupersedeStaleContextJobs :exec
UPDATE ml_jobs
SET status = 'superseded',
    last_error = 'transcript fingerprint changed',
    locked_at = NULL,
    locked_by = '',
    updated_at = now()
WHERE video_id = sqlc.arg(video_id)
  AND kind = 'context_windows'
  AND transcript_hash <> sqlc.arg(transcript_hash)
  AND status IN ('queued', 'paused', 'cancelled', 'retry_wait', 'waiting_model');

-- name: GetTranscriptFingerprint :one
SELECT transcript_fingerprint(sqlc.arg(video_id)::uuid)::text AS fingerprint;

-- name: LockMLPublication :one
SELECT id FROM ml_jobs WHERE id=sqlc.arg(id) AND lease_token=sqlc.arg(lease_token) AND status='processing' FOR UPDATE;

-- name: GetContextChunk :one
SELECT output FROM context_window_chunks WHERE set_id=sqlc.arg(set_id) AND ordinal=sqlc.arg(ordinal);

-- name: SaveContextChunk :exec
INSERT INTO context_window_chunks(set_id,ordinal,output) VALUES(sqlc.arg(set_id),sqlc.arg(ordinal),sqlc.arg(output)) ON CONFLICT DO NOTHING;

-- name: LockVideoForContext :one
SELECT id FROM videos WHERE id=sqlc.arg(id) FOR UPDATE;

-- name: LockTranscriptForContext :exec
SELECT video_id FROM video_transcripts WHERE video_id=sqlc.arg(video_id) FOR SHARE;

-- name: HasHigherPriorityMLJob :one
SELECT EXISTS(SELECT 1 FROM ml_jobs WHERE status IN ('queued','retry_wait') AND retry_at<=now() AND priority<sqlc.arg(priority))::boolean;

-- CooldownMLKind parks every claimable row of a kind so ClaimMLJob will not
-- pick them until ml_runtime_health.retry_at (and this retry_at) expire.
-- queued/retry_wait become waiting_model; attempts are not touched.
-- name: CooldownMLKind :exec
UPDATE ml_jobs
SET retry_at = greatest(retry_at, now() + interval '15 minutes'),
    status = CASE WHEN status IN ('queued', 'retry_wait') THEN 'waiting_model' ELSE status END
WHERE kind = sqlc.arg(kind)
  AND status IN ('queued', 'retry_wait', 'waiting_model');

-- RecordMLRuntimeFailure cools the kind immediately. First insert used to
-- default retry_at=now(), so ClaimMLJob reclaimed transcribe every minute.
-- name: RecordMLRuntimeFailure :exec
INSERT INTO ml_runtime_health(kind, failures, last_error, retry_at)
VALUES (sqlc.arg(kind), 1, sqlc.arg(last_error), now() + interval '15 minutes')
ON CONFLICT (kind) DO UPDATE SET
    failures = ml_runtime_health.failures + 1,
    last_error = EXCLUDED.last_error,
    retry_at = now() + interval '15 minutes',
    updated_at = now();

-- name: RecordMLRuntimeSuccess :exec
INSERT INTO ml_runtime_health(kind,verified_at) VALUES(sqlc.arg(kind),now())
ON CONFLICT(kind) DO UPDATE SET failures=0,last_error='',retry_at=now(),verified_at=now(),updated_at=now();

-- EnqueueDemandContextJobs queues context for videos people use: clips, markers,
-- playback, and files archived in the last week. Catalog-only rows are skipped.
-- name: EnqueueDemandContextJobs :exec
INSERT INTO ml_jobs(video_id,kind,priority,transcript_hash,model_digest,prompt_version)
SELECT t.video_id,
       'context_windows',
       CASE
           WHEN EXISTS (SELECT 1 FROM clips c WHERE c.video_id = t.video_id) THEN 120
           WHEN EXISTS (SELECT 1 FROM playback_positions p WHERE p.video_id = t.video_id) THEN 130
           ELSE 160
       END,
       transcript_fingerprint(t.video_id),
       sqlc.arg(model_digest),
       sqlc.arg(prompt_version)
FROM video_transcripts t
JOIN videos v ON v.id = t.video_id
WHERE t.text <> ''
  AND (
      EXISTS (SELECT 1 FROM clips c WHERE c.video_id = t.video_id)
      OR EXISTS (SELECT 1 FROM playback_positions p WHERE p.video_id = t.video_id)
      OR (v.media = 'file' AND v.created_at > now() - interval '7 days')
  )
  AND NOT EXISTS (
      SELECT 1 FROM context_window_sets s
      WHERE s.video_id = t.video_id
        AND s.transcript_hash = transcript_fingerprint(t.video_id)
        AND s.prompt_version = sqlc.arg(prompt_version)
        AND s.status = 'succeeded'
  )
ON CONFLICT (video_id, kind, transcript_hash, model_digest, prompt_version) DO UPDATE
SET status = 'queued',
    retry_at = now(),
    last_error = '',
    locked_at = NULL,
    locked_by = '',
    lease_token = NULL,
    updated_at = now()
WHERE ml_jobs.status IN ('cancelled', 'failed', 'paused');

-- ResumeDemandContextJobs unpauses parked demand videos after an archive clear.
-- name: ResumeDemandContextJobs :exec
UPDATE ml_jobs j
SET status = 'queued',
    retry_at = now(),
    updated_at = now(),
    priority = LEAST(j.priority, CASE
        WHEN EXISTS (SELECT 1 FROM clips c WHERE c.video_id = j.video_id) THEN 120
        WHEN EXISTS (SELECT 1 FROM playback_positions p WHERE p.video_id = j.video_id) THEN 130
        ELSE 160
    END)
WHERE j.kind = 'context_windows'
  AND j.status = 'paused'
  AND EXISTS (SELECT 1 FROM video_transcripts t WHERE t.video_id = j.video_id AND t.text <> '')
  AND (
      EXISTS (SELECT 1 FROM clips c WHERE c.video_id = j.video_id)
      OR EXISTS (SELECT 1 FROM playback_positions p WHERE p.video_id = j.video_id)
      OR EXISTS (
          SELECT 1 FROM videos v
          WHERE v.id = j.video_id
            AND v.media = 'file'
            AND v.created_at > now() - interval '7 days'
      )
  );

-- CancelClaimableMLJobs takes queued, waiting, and in-flight jobs off the
-- line. Heartbeat then drops the worker. Explicit retry/enqueue can revive.
-- name: CancelClaimableMLJobs :execrows
UPDATE ml_jobs
SET status = 'cancelled',
    locked_at = NULL,
    locked_by = '',
    lease_token = NULL,
    last_error = CASE WHEN last_error = '' THEN 'cancelled' ELSE last_error END,
    updated_at = now()
WHERE status IN ('queued', 'processing', 'waiting_model', 'waiting_assets', 'retry_wait', 'paused')
  AND (sqlc.arg(kind) = '' OR kind = sqlc.arg(kind))
  AND priority >= sqlc.arg(min_priority);

-- CancelMLJob cancels one non-terminal job, including an in-flight run.
-- name: CancelMLJob :execrows
UPDATE ml_jobs
SET status = 'cancelled',
    locked_at = NULL,
    locked_by = '',
    lease_token = NULL,
    last_error = CASE WHEN last_error = '' THEN 'cancelled' ELSE last_error END,
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND status IN ('queued', 'processing', 'waiting_model', 'waiting_assets', 'retry_wait', 'paused');

-- SetMLJobPriority reorders a claimable job. Lower runs first.
-- name: SetMLJobPriority :execrows
UPDATE ml_jobs
SET priority = sqlc.arg(priority),
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND status IN ('queued', 'retry_wait', 'waiting_model', 'waiting_assets');

-- name: ListMLRuntimeHealth :many
SELECT * FROM ml_runtime_health ORDER BY kind;

-- RetryMLJob requeues one operator-selected job without changing its uniqueness key.
-- name: RetryMLJob :execrows
UPDATE ml_jobs
SET status = 'queued',
    failure_count = 0,
    retry_at = now(),
    last_error = '',
    locked_at = NULL,
    locked_by = '',
    lease_token = NULL,
    updated_at = NOW()
WHERE id = sqlc.arg(id)
  AND status IN ('failed', 'retry_wait', 'waiting_model', 'waiting_assets', 'superseded', 'paused', 'cancelled');
