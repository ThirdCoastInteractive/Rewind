-- GetActiveAssetJobsForVideo returns active (queued/processing) ingest jobs
-- for a given video, including both normal post-ingest and regen jobs.
-- asset_scope is NULL or 'all' for all derived assets, or one of thumbnail/preview/seek/waveform.
-- name: GetActiveAssetJobsForVideo :many
SELECT ij.id AS ingest_job_id,
       ij.asset_scope,
       ij.status
FROM ingest_jobs ij
JOIN download_jobs dj ON dj.id = ij.download_job_id
WHERE dj.video_id = sqlc.arg(video_id)
  AND ij.status IN ('queued', 'processing');

-- EnqueueDownloadJob inserts a new download job.
-- name: EnqueueDownloadJob :one
INSERT INTO download_jobs (
    url,
    archived_by,
    status,
    refresh,
    extra_args
)
VALUES (
    sqlc.arg(url),
    sqlc.arg(archived_by),
    'queued',
    sqlc.arg(refresh),
    sqlc.arg(extra_args)
)
RETURNING *;

-- EnqueueDownloadJobOnce atomically reuses a queued, processing, or succeeded
-- top-level video job for the same canonical URL. Explicit redownload and
-- format-selection actions use EnqueueDownloadJob instead.
-- name: EnqueueDownloadJobOnce :one
WITH existing AS (
    SELECT dj.id
    FROM download_jobs dj
    WHERE dj.url = sqlc.arg(url)
      AND dj.kind = 'video'
      AND dj.parent_job_id IS NULL
      AND dj.status IN ('queued', 'processing', 'succeeded')
    ORDER BY
      CASE dj.status WHEN 'processing' THEN 0 WHEN 'queued' THEN 1 ELSE 2 END,
      dj.created_at DESC
    LIMIT 1
),
inserted AS (
    INSERT INTO download_jobs (url, archived_by, status, refresh, extra_args, dedupe_key)
    SELECT sqlc.arg(url), sqlc.arg(archived_by), 'queued', sqlc.arg(refresh), sqlc.arg(extra_args), sqlc.arg(url)
    WHERE NOT EXISTS (SELECT 1 FROM existing)
    ON CONFLICT (dedupe_key) WHERE dedupe_key IS NOT NULL AND status IN ('queued', 'processing')
    DO UPDATE SET dedupe_key = EXCLUDED.dedupe_key
    RETURNING id, (xmax <> 0) AS reused
)
SELECT id, reused FROM inserted
UNION ALL
SELECT id, TRUE AS reused FROM existing
LIMIT 1;

-- GetReusableDownloadJobForVideo returns an existing useful job for an
-- already-archived video instead of creating another download row.
-- name: GetReusableDownloadJobForVideo :one
SELECT *
FROM download_jobs
WHERE (video_id = sqlc.arg(video_id) OR url = sqlc.arg(video_src))
  AND status IN ('queued', 'processing', 'succeeded')
ORDER BY
  CASE status WHEN 'processing' THEN 0 WHEN 'queued' THEN 1 ELSE 2 END,
  created_at DESC
LIMIT 1;

-- DequeueDownloadJob claims one queued download job. Playlist/channel-scan
-- jobs are claimed before video downloads: they are quick flat enumerations
-- whose expansion feeds the queue, and letting them ride FIFO behind a large
-- download backlog would delay watch scans (and playlist expansion) by hours.
-- name: DequeueDownloadJob :one
WITH cte AS (
    SELECT id
    FROM download_jobs
    WHERE status = 'queued'
    ORDER BY (CASE
        WHEN kind IN ('playlist', 'metadata-catalog') THEN 0
        WHEN kind = 'metadata' THEN 2
        ELSE 1
      END), created_at
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
UPDATE download_jobs
SET status = 'processing',
    attempts = attempts + 1,
    started_at = COALESCE(started_at, NOW()),
    updated_at = NOW()
WHERE id IN (SELECT id FROM cte)
RETURNING *;

-- MarkDownloadJobSucceeded stores paths and marks job done.
-- name: MarkDownloadJobSucceeded :exec
UPDATE download_jobs
SET status = 'succeeded',
    finished_at = NOW(),
    updated_at = NOW(),
    spool_dir = sqlc.arg(spool_dir),
    info_json_path = sqlc.arg(info_json_path),
    last_error = NULL
WHERE id = sqlc.arg(id);

-- MarkDownloadJobFailed stores error and marks job failed.
-- name: MarkDownloadJobFailed :exec
UPDATE download_jobs
SET status = 'failed',
    finished_at = NOW(),
    updated_at = NOW(),
    last_error = sqlc.arg(last_error)
WHERE id = sqlc.arg(id);

-- EnqueueIngestJob inserts a new ingest job from a download job.
-- name: EnqueueIngestJob :one
INSERT INTO ingest_jobs (
    download_job_id,
    status
)
VALUES (
    sqlc.arg(download_job_id),
    'queued'
)
RETURNING *;

-- EnqueuePostIngestAssetsJob queues derived-asset generation on the same
-- download job after ingest has published playable media. asset_scope='all'
-- is what separates this claim from the parent ingest job.
-- name: EnqueuePostIngestAssetsJob :one
INSERT INTO ingest_jobs (
    download_job_id,
    status,
    asset_scope
)
VALUES (
    sqlc.arg(download_job_id),
    'queued',
    'all'
)
RETURNING *;

-- RecoverStuckIngestJobs resets orphaned "processing" jobs back to "queued" on service startup.
-- Jobs stuck in "processing" for more than the timeout are assumed to have been orphaned by a crash.
-- name: RecoverStuckIngestJobs :exec
UPDATE ingest_jobs
SET status = 'queued',
    updated_at = NOW()
WHERE status = 'processing'
  AND updated_at < NOW() - INTERVAL '5 minutes';

-- FailExcessiveRetryIngestJobs permanently fails jobs that have been retried too many times.
-- This prevents zombie jobs from wasting workers indefinitely.
-- name: FailExcessiveRetryIngestJobs :execrows
UPDATE ingest_jobs
SET status = 'failed',
    last_error = 'exceeded maximum retry attempts',
    finished_at = NOW(),
    updated_at = NOW()
WHERE status = 'queued'
  AND attempts >= 5;

-- DequeueIngestJob claims one queued ingest or derived-asset job.
-- asset_jobs=false claims media ingest: spool/info.json and no asset_scope.
-- asset_jobs=true claims generation: no spool, or asset_scope set (including
-- post-ingest 'all' jobs that share the original download row).
-- Returns video_id for asset regeneration jobs (NULL until ingest links it).
-- Skips jobs that have already been retried too many times.
-- name: DequeueIngestJob :one
WITH cte AS (
    SELECT ij.id
    FROM ingest_jobs ij
    JOIN download_jobs dj ON dj.id = ij.download_job_id
    WHERE ij.status = 'queued'
      AND ij.attempts < 5
      AND (
        CASE WHEN sqlc.arg(asset_jobs)::boolean THEN
          (
            (
              (dj.info_json_path IS NULL OR btrim(dj.info_json_path) = '')
              AND (dj.spool_dir IS NULL OR btrim(dj.spool_dir) = '')
            )
            OR (ij.asset_scope IS NOT NULL AND btrim(ij.asset_scope) <> '')
          )
        ELSE
          (
            (
              (dj.info_json_path IS NOT NULL AND btrim(dj.info_json_path) <> '')
              OR (dj.spool_dir IS NOT NULL AND btrim(dj.spool_dir) <> '')
            )
            AND (ij.asset_scope IS NULL OR btrim(ij.asset_scope) = '')
          )
        END
      )
    ORDER BY ij.created_at
    LIMIT 1
    FOR UPDATE OF ij SKIP LOCKED
)
UPDATE ingest_jobs AS ij
SET status = 'processing',
    attempts = ij.attempts + 1,
    started_at = COALESCE(ij.started_at, NOW()),
    updated_at = NOW()
FROM download_jobs AS dj
WHERE ij.id IN (SELECT id FROM cte)
  AND dj.id = ij.download_job_id
RETURNING
    ij.id AS ingest_job_id,
    ij.download_job_id,
    dj.url AS url,
    dj.archived_by AS archived_by,
    dj.refresh AS refresh,
    dj.spool_dir AS spool_dir,
    dj.info_json_path AS info_json_path,
    dj.video_id AS video_id,
    ij.asset_scope AS asset_scope,
    dj.extra_args AS extra_args,
    dj.kind AS kind;

-- HeartbeatIngestJob touches updated_at to prevent the recovery goroutine from
-- resetting a long-running job back to "queued" while it is still being processed.
-- name: HeartbeatIngestJob :exec
UPDATE ingest_jobs
SET updated_at = NOW()
WHERE id = sqlc.arg(id)
  AND status = 'processing';

-- MarkIngestJobSucceeded marks ingest done.
-- name: MarkIngestJobSucceeded :exec
UPDATE ingest_jobs
SET status = 'succeeded',
    finished_at = NOW(),
    updated_at = NOW(),
    last_error = NULL
WHERE id = sqlc.arg(id);

-- MarkIngestJobFailed marks ingest failed.
-- name: MarkIngestJobFailed :exec
UPDATE ingest_jobs
SET status = 'failed',
    finished_at = NOW(),
    updated_at = NOW(),
    last_error = sqlc.arg(last_error)
WHERE id = sqlc.arg(id);

-- LinkDownloadJobVideo stores the created video id.
-- name: LinkDownloadJobVideo :exec
UPDATE download_jobs
SET video_id = sqlc.arg(video_id),
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- RetryDownloadJob resets a job to queued status for retry.
-- name: RetryDownloadJob :exec
UPDATE download_jobs
SET status = 'queued',
    last_error = NULL,
    started_at = NULL,
    finished_at = NULL,
    process_pid = NULL,
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- RecoverStuckDownloadJobs resets orphaned "processing" jobs back to "queued" on service startup.
-- Jobs stuck in "processing" for more than the timeout are assumed to have been orphaned by a crash or restart.
-- name: RecoverStuckDownloadJobs :exec
UPDATE download_jobs
SET status = 'queued',
    updated_at = NOW()
WHERE status = 'processing'
  AND updated_at < NOW() - INTERVAL '5 minutes';

-- UpdateDownloadJobPID stores the process ID of the running download.
-- name: UpdateDownloadJobPID :exec
UPDATE download_jobs
SET process_pid = sqlc.arg(process_pid),
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- CancelDownloadJob marks a job as cancelled.
-- name: CancelDownloadJob :exec
UPDATE download_jobs
SET status = 'failed',
    last_error = 'Cancelled by user',
    finished_at = NOW(),
    process_pid = NULL,
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- GetDownloadJobPID retrieves the process ID for a job.
-- name: GetDownloadJobPID :one
SELECT process_pid
FROM download_jobs
WHERE id = sqlc.arg(id);

-- ArchiveJob marks a job as archived (soft delete).
-- name: ArchiveJob :exec
UPDATE download_jobs
SET archived = TRUE,
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- UnarchiveJob unmarks a job from archived status.
-- name: UnarchiveJob :exec
UPDATE download_jobs
SET archived = FALSE,
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- ArchiveJobs marks multiple jobs as archived (batch operation).
-- name: ArchiveJobs :exec
UPDATE download_jobs
SET archived = TRUE,
    updated_at = NOW()
WHERE id = ANY(sqlc.arg(job_ids)::uuid[]);

-- EnqueueUploadIngestJob creates a download + ingest job pair for a local file upload.
-- The download_job is pre-marked as succeeded (no yt-dlp download needed).
-- name: EnqueueUploadIngestJob :one
WITH new_download_job AS (
    INSERT INTO download_jobs (
        url,
        archived_by,
        status,
        refresh,
        spool_dir,
        info_json_path,
        finished_at
    )
    VALUES (
        sqlc.arg(url),
        sqlc.arg(archived_by),
        'succeeded',
        false,
        sqlc.arg(spool_dir),
        sqlc.arg(info_json_path),
        NOW()
    )
    RETURNING *
),
new_ingest_job AS (
    INSERT INTO ingest_jobs (
        download_job_id,
        status
    )
    SELECT new_download_job.id, 'queued'
    FROM new_download_job
    RETURNING *
)
SELECT
    new_ingest_job.id AS ingest_job_id,
    new_download_job.id AS download_job_id
FROM new_ingest_job, new_download_job;

-- EnqueueAssetRegenerationJob creates a download + ingest job pair for regenerating assets.
-- asset_scope: NULL = all assets, or one of 'thumbnail', 'preview', 'seek', 'waveform'.
-- name: EnqueueAssetRegenerationJob :one
WITH new_download_job AS (
    INSERT INTO download_jobs (
        url,
        archived_by,
        refresh,
        status,
        video_id
    )
    SELECT
        v.src,
        v.archived_by,
        true,
        'succeeded',
        v.id
    FROM videos v
    WHERE v.id = sqlc.arg(video_id)
    RETURNING *
),
new_ingest_job AS (
    INSERT INTO ingest_jobs (
        download_job_id,
        status,
        asset_scope
    )
    SELECT
        new_download_job.id,
        'queued',
        sqlc.narg(asset_scope)::text
    FROM new_download_job
    RETURNING *
)
SELECT
    new_ingest_job.id AS ingest_job_id,
    new_download_job.id AS download_job_id,
    new_download_job.video_id AS video_id
FROM new_ingest_job, new_download_job;

-- EnqueuePlaylistJob inserts a parent "playlist" job. The downloader expands it
-- into child video jobs (see EnqueueChildDownloadJobs) rather than downloading.
-- name: EnqueuePlaylistJob :one
INSERT INTO download_jobs (
    url,
    archived_by,
    status,
    kind
)
VALUES (
    sqlc.arg(url),
    sqlc.arg(archived_by),
    'queued',
    'playlist'
)
RETURNING *;

-- EnqueueChildDownloadJobs bulk-inserts one normal video download job per URL,
-- all linked to a parent playlist job. Each insert fires the download_jobs
-- NOTIFY trigger, so existing downloader workers pick them up unchanged.
-- name: EnqueueChildDownloadJobs :execrows
INSERT INTO download_jobs (url, archived_by, status, kind, parent_job_id)
SELECT u, sqlc.arg(archived_by), 'queued', 'video', sqlc.arg(parent_job_id)
FROM unnest(sqlc.arg(urls)::text[]) AS u;

-- EnqueueMetadataCatalogJob inserts a parent job that fans out metadata-only children.
-- name: EnqueueMetadataCatalogJob :one
INSERT INTO download_jobs (
    url,
    archived_by,
    status,
    kind
)
VALUES (
    sqlc.arg(url),
    sqlc.arg(archived_by),
    'queued',
    'metadata-catalog'
)
RETURNING *;

-- EnqueueChildMetadataJobs bulk-inserts skip-download metadata jobs for catalog expansion.
-- name: EnqueueChildMetadataJobs :execrows
INSERT INTO download_jobs (url, archived_by, status, kind, parent_job_id)
SELECT u, sqlc.arg(archived_by), 'queued', 'metadata', sqlc.arg(parent_job_id)
FROM unnest(sqlc.arg(urls)::text[]) AS u;

-- CompletePlaylistJob marks a playlist parent job done after fan-out and records
-- how many child jobs were enqueued (batch_total) and a human label (batch_label).
-- name: CompletePlaylistJob :exec
UPDATE download_jobs
SET status = 'succeeded',
    finished_at = NOW(),
    updated_at = NOW(),
    batch_total = sqlc.arg(batch_total),
    batch_label = sqlc.arg(batch_label),
    last_error = NULL
WHERE id = sqlc.arg(id);
