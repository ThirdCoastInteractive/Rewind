-- name: CreateCatalogCrawl :one
INSERT INTO catalog_crawls (channel_id, requested_by, feed_url, feed_kind, platform, refresh)
VALUES (sqlc.arg(channel_id), sqlc.arg(requested_by), sqlc.arg(feed_url), sqlc.arg(feed_kind), sqlc.arg(platform), sqlc.arg(refresh))
ON CONFLICT (channel_id, feed_kind) WHERE status IN ('queued', 'running', 'paused', 'retry_wait')
DO UPDATE SET refresh = catalog_crawls.refresh OR EXCLUDED.refresh, updated_at = NOW()
RETURNING *;

-- name: GetCatalogCrawl :one
SELECT * FROM catalog_crawls WHERE id = sqlc.arg(id);

-- name: ListCatalogCrawlsForChannel :many
SELECT * FROM catalog_crawls WHERE channel_id = sqlc.arg(channel_id)
ORDER BY created_at DESC;

-- name: ListCatalogCrawlsForCreator :many
SELECT cc.*, COALESCE(ch.uploader, ch.identity_key) AS channel_name
FROM catalog_crawls cc
JOIN channels ch ON ch.id = cc.channel_id
WHERE ch.creator_id = sqlc.arg(creator_id)
ORDER BY cc.created_at DESC;

-- name: ClaimCatalogCrawl :one
UPDATE catalog_crawls SET status = 'running', locked_at = NOW(), locked_by = sqlc.arg(worker_id),
    attempts = attempts + 1, updated_at = NOW()
WHERE id = (
    SELECT c.id FROM catalog_crawls c
    WHERE c.status IN ('queued', 'retry_wait')
      AND (c.retry_at IS NULL OR c.retry_at <= NOW())
      AND NOT EXISTS (
        SELECT 1 FROM catalog_crawls active
        WHERE active.platform = c.platform AND active.status = 'running'
          AND active.locked_at > NOW() - INTERVAL '15 minutes'
      )
    ORDER BY c.created_at
    LIMIT 1 FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: AdvanceCatalogCrawl :exec
UPDATE catalog_crawls SET next_page_index = sqlc.arg(next_page_index),
    entries_seen = entries_seen + sqlc.arg(entries_seen),
    entries_added = entries_added + sqlc.arg(entries_added),
    entries_updated = entries_updated + sqlc.arg(entries_updated),
    locked_at = NOW(), updated_at = NOW()
WHERE id = sqlc.arg(id) AND status = 'running';

-- name: SetCatalogCrawlStatus :exec
UPDATE catalog_crawls SET status = sqlc.arg(status), last_error = sqlc.arg(last_error),
    retry_at = sqlc.narg(retry_at), locked_at = NULL, locked_by = '',
    finished_at = CASE WHEN sqlc.arg(status)::text IN ('complete', 'cancelled', 'failed') THEN NOW() ELSE finished_at END,
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- SetRunningCatalogCrawlStatus only advances a crawl that is still owned by a
-- worker. A pause or cancel issued while yt-dlp is running therefore wins.
-- name: SetRunningCatalogCrawlStatus :exec
UPDATE catalog_crawls SET status = sqlc.arg(status), last_error = sqlc.arg(last_error),
    retry_at = sqlc.narg(retry_at), locked_at = NULL, locked_by = '',
    finished_at = CASE WHEN sqlc.arg(status)::text IN ('complete', 'failed') THEN NOW() ELSE finished_at END,
    updated_at = NOW()
WHERE id = sqlc.arg(id) AND status = 'running';

-- name: RecoverCatalogCrawls :exec
UPDATE catalog_crawls SET status = 'queued', locked_at = NULL, locked_by = '', updated_at = NOW()
WHERE status = 'running' AND (locked_at IS NULL OR locked_at < NOW() - INTERVAL '15 minutes');

-- name: SetVideoSubtitleState :exec
UPDATE videos SET subtitle_state = sqlc.arg(subtitle_state), subtitle_checked_at = NOW(),
    subtitle_last_error = sqlc.arg(last_error), updated_at = NOW()
WHERE id = sqlc.arg(id);

-- name: ListVideosNeedingSubtitles :many
SELECT id, src FROM videos
WHERE NOT EXISTS (SELECT 1 FROM video_transcripts vt WHERE vt.video_id = videos.id)
  AND subtitle_state <> 'unavailable'
ORDER BY subtitle_checked_at NULLS FIRST, created_at
LIMIT sqlc.arg(page_limit);

-- name: ListCatalogCandidateStates :many
SELECT v.id, v.subtitle_state,
       EXISTS (SELECT 1 FROM video_transcripts vt WHERE vt.video_id = v.id) AS has_transcript
FROM videos v WHERE v.id = ANY(sqlc.arg(ids)::uuid[]);

-- name: EnqueueCatalogMetadataJobs :execrows
INSERT INTO download_jobs (url, archived_by, status, kind)
SELECT u, sqlc.arg(archived_by), 'queued', 'metadata'
FROM unnest(sqlc.arg(urls)::text[]) AS u;

-- name: EnqueueSubtitleBackfillJobs :execrows
INSERT INTO download_jobs (url, archived_by, status, kind)
SELECT v.src, v.archived_by, 'queued', 'metadata'
FROM videos v
WHERE NOT EXISTS (SELECT 1 FROM video_transcripts vt WHERE vt.video_id = v.id)
  AND v.subtitle_state <> 'unavailable'
  AND NOT EXISTS (
      SELECT 1 FROM download_jobs dj
      WHERE (dj.video_id = v.id OR dj.url = v.src)
        AND dj.status IN ('queued', 'processing')
  )
ORDER BY v.subtitle_checked_at NULLS FIRST, v.created_at
LIMIT sqlc.arg(page_limit);
