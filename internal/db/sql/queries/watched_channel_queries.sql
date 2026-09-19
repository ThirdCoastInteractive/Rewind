-- CreateWatchedChannel registers a channel/playlist URL for periodic scanning.
-- name: CreateWatchedChannel :one
INSERT INTO watched_channels (
    created_by,
    url,
    label,
    cron_schedule,
    backfill,
    next_scan_at,
    channel_id
)
VALUES (
    sqlc.arg(created_by),
    sqlc.arg(url),
    sqlc.arg(label),
    sqlc.arg(cron_schedule),
    sqlc.arg(backfill),
    sqlc.arg(next_scan_at),
    sqlc.narg(channel_id)
)
RETURNING *;

-- GetWatchedChannelByUserAndChannel returns a follow for this user attached
-- to the given channels row, if one exists.
-- name: GetWatchedChannelByUserAndChannel :one
SELECT * FROM watched_channels WHERE created_by = $1 AND channel_id = $2 LIMIT 1;

-- SetWatchedChannelChannelID attaches a follow to a first-class channel row.
-- name: SetWatchedChannelChannelID :exec
UPDATE watched_channels SET channel_id = $2, updated_at = NOW() WHERE id = $1;

-- LinkWatchedChannelsToChannels backfills channel_id on follows whose URL or
-- label already matches a materialized channel.
-- name: LinkWatchedChannelsToChannels :exec
UPDATE watched_channels w SET channel_id = c.id
FROM channels c
WHERE w.channel_id IS NULL AND (
  c.canonical_url = w.url OR (w.label <> '' AND c.uploader = w.label)
);

-- ListWatchedChannels returns all watches with creator name and how many
-- videos each watch has enqueued so far, newest watch first.
-- name: ListWatchedChannels :many
SELECT w.*,
       u.user_name AS created_by_name,
       COALESCE(t.tracked, 0)::bigint AS tracked_count
FROM watched_channels w
JOIN users u ON u.id = w.created_by
LEFT JOIN LATERAL (
    SELECT COUNT(*) AS tracked
    FROM watched_channel_videos v
    WHERE v.watch_id = w.id AND v.enqueued
) t ON TRUE
ORDER BY w.created_at DESC;

-- GetWatchedChannel returns one watch by id.
-- name: GetWatchedChannel :one
SELECT * FROM watched_channels WHERE id = sqlc.arg(id);

-- DeleteWatchedChannel removes a watch and its seen-ledger (cascade).
-- Archived videos and past download jobs are untouched.
-- name: DeleteWatchedChannel :exec
DELETE FROM watched_channels WHERE id = sqlc.arg(id);

-- SetWatchedChannelEnabled toggles scanning for a watch.
-- name: SetWatchedChannelEnabled :exec
UPDATE watched_channels
SET enabled = sqlc.arg(enabled),
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- UpdateWatchedChannelSchedule stores an edited label/schedule and the
-- recomputed next run time.
-- name: UpdateWatchedChannelSchedule :exec
UPDATE watched_channels
SET label = sqlc.arg(label),
    cron_schedule = sqlc.arg(cron_schedule),
    next_scan_at = sqlc.arg(next_scan_at),
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- RequestWatchedChannelScan makes a watch due immediately ("scan now").
-- name: RequestWatchedChannelScan :exec
UPDATE watched_channels
SET next_scan_at = NOW(),
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- ClaimDueWatchedChannels locks due, enabled watches for scheduling. Must run
-- inside a transaction: SKIP LOCKED keeps multiple downloader replicas from
-- double-scheduling the same watch. Watches whose previous scan job is still
-- queued/processing are skipped so slow scans never stack.
-- name: ClaimDueWatchedChannels :many
SELECT w.* FROM watched_channels w
WHERE w.enabled
  AND w.next_scan_at <= NOW()
  AND NOT EXISTS (
      SELECT 1 FROM download_jobs dj
      WHERE dj.watch_id = w.id
        AND dj.kind = 'playlist'
        AND dj.status IN ('queued', 'processing')
  )
ORDER BY w.next_scan_at
LIMIT sqlc.arg(max_watches)
FOR UPDATE OF w SKIP LOCKED;

-- ScheduleWatchedChannelNextScan stores the next cron occurrence once a scan
-- has been scheduled.
-- name: ScheduleWatchedChannelNextScan :exec
UPDATE watched_channels
SET next_scan_at = sqlc.arg(next_scan_at),
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- EnqueueWatchScanJob inserts the parent scan job for a watch. It is a normal
-- playlist-kind download job; the watch_id tag routes it through watch-aware
-- expansion in the downloader.
-- name: EnqueueWatchScanJob :one
INSERT INTO download_jobs (url, archived_by, status, kind, watch_id)
VALUES (sqlc.arg(url), sqlc.arg(archived_by), 'queued', 'playlist', sqlc.arg(watch_id))
RETURNING *;

-- RecordWatchedChannelScanResult stores the outcome of a scan run.
-- name: RecordWatchedChannelScanResult :exec
UPDATE watched_channels
SET last_scan_at = NOW(),
    last_scan_status = sqlc.arg(status),
    last_scan_error = sqlc.narg(last_error),
    last_scan_found = sqlc.arg(found),
    first_scan_done = first_scan_done OR sqlc.arg(mark_first_done)::boolean,
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- SetWatchedChannelLabelIfEmpty fills the label from channel metadata on the
-- first scan, unless the user already provided one.
-- name: SetWatchedChannelLabelIfEmpty :exec
UPDATE watched_channels
SET label = sqlc.arg(label),
    updated_at = NOW()
WHERE id = sqlc.arg(id) AND btrim(label) = '';

-- FilterSeenWatchedChannelVideos returns, from the candidate ids, the subset
-- already recorded in the watch's seen-ledger.
-- name: FilterSeenWatchedChannelVideos :many
SELECT video_id FROM watched_channel_videos
WHERE watch_id = sqlc.arg(watch_id)
  AND video_id = ANY(sqlc.arg(ids)::uuid[]);

-- RecordWatchedChannelVideos inserts newly observed entries into the
-- seen-ledger (batched into one round trip by pgx). Conflicts are ignored.
-- name: RecordWatchedChannelVideos :batchexec
INSERT INTO watched_channel_videos (watch_id, video_id, entry_id, url, title, enqueued)
VALUES (
    sqlc.arg(watch_id),
    sqlc.arg(video_id),
    sqlc.arg(entry_id),
    sqlc.arg(url),
    sqlc.arg(title),
    sqlc.arg(enqueued)
)
ON CONFLICT (watch_id, video_id) DO NOTHING;

-- name: DeleteOwnedWatchedChannel :execrows
DELETE FROM watched_channels WHERE id=sqlc.arg(id) AND created_by=sqlc.arg(created_by);

