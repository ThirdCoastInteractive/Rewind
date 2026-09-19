-- name: CreateContextWindow :one
INSERT INTO context_windows (
    video_id, start_ts, end_ts, title, summary, topics, entities, search,
    origin, source_query, transcript_cue_evidence, transcript_version,
    boundary_quality, created_by
)
SELECT
    sqlc.arg(video_id), sqlc.arg(start_ts), sqlc.arg(end_ts), sqlc.arg(title),
    sqlc.arg(summary), sqlc.arg(topics), sqlc.arg(entities),
    context_window_search_vector(sqlc.arg(title), sqlc.arg(summary), sqlc.arg(topics), sqlc.arg(entities)),
    sqlc.arg(origin), sqlc.arg(source_query), sqlc.arg(transcript_cue_evidence),
    v.transcript_version, sqlc.arg(boundary_quality), sqlc.arg(created_by)
FROM videos v WHERE v.id = sqlc.arg(video_id)
RETURNING *;

-- name: UpdateContextWindow :one
UPDATE context_windows SET
    override_title = override_title OR title IS DISTINCT FROM sqlc.arg(title),
    override_summary = override_summary OR summary IS DISTINCT FROM sqlc.arg(summary) OR topics IS DISTINCT FROM sqlc.arg(topics) OR entities IS DISTINCT FROM sqlc.arg(entities),
    override_bounds = override_bounds OR start_ts IS DISTINCT FROM sqlc.arg(start_ts) OR end_ts IS DISTINCT FROM sqlc.arg(end_ts),
    start_ts = sqlc.arg(start_ts), end_ts = sqlc.arg(end_ts), title = sqlc.arg(title),
    summary = sqlc.arg(summary), topics = sqlc.arg(topics), entities = sqlc.arg(entities),
    search = context_window_search_vector(sqlc.arg(title), sqlc.arg(summary), sqlc.arg(topics), sqlc.arg(entities)),
    boundary_quality = sqlc.arg(boundary_quality), updated_at = NOW()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteContextWindow :exec
DELETE FROM context_windows WHERE id = sqlc.arg(id);

-- name: LockContextWindow :one
SELECT id FROM context_windows WHERE id=sqlc.arg(id) FOR UPDATE;

-- name: GetContextWindow :one
SELECT cw.*, (cw.transcript_version <> v.transcript_version) AS evidence_stale
FROM context_windows cw JOIN videos v ON v.id = cw.video_id
WHERE cw.id = sqlc.arg(id);

-- name: ListContextWindowsForVideo :many
SELECT cw.*, (cw.transcript_version <> v.transcript_version) AS evidence_stale
FROM context_windows cw JOIN videos v ON v.id = cw.video_id
WHERE cw.video_id = sqlc.arg(video_id)
  AND cw.stale = FALSE
  AND (sqlc.narg('start_ts')::float8 IS NULL OR cw.end_ts >= sqlc.narg('start_ts'))
  AND (sqlc.narg('end_ts')::float8 IS NULL OR cw.start_ts <= sqlc.narg('end_ts'))
ORDER BY cw.start_ts, cw.end_ts, cw.created_at;

-- name: SearchContextWindows :many
SELECT cw.*, v.title AS video_title, v.uploader, v.media,
       (cw.transcript_version <> v.transcript_version) AS evidence_stale,
       ts_rank_cd(cw.search, to_tsquery('simple', sqlc.arg(tsquery))) AS rank
FROM context_windows cw
JOIN videos v ON v.id = cw.video_id
LEFT JOIN channels ch ON ch.id = v.channel_row_id
WHERE cw.search @@ to_tsquery('simple', sqlc.arg(tsquery))
  AND cw.stale = FALSE
  AND (sqlc.narg('creator_id')::uuid IS NULL OR ch.creator_id = sqlc.narg('creator_id'))
  AND (sqlc.narg('channel_id')::uuid IS NULL OR ch.id = sqlc.narg('channel_id'))
ORDER BY rank DESC, cw.updated_at DESC
LIMIT sqlc.arg(page_limit);

-- name: ListContextWindowsForVideos :many
SELECT cw.*, (cw.transcript_version <> v.transcript_version) AS evidence_stale
FROM context_windows cw JOIN videos v ON v.id=cw.video_id
WHERE cw.video_id=ANY(sqlc.arg(video_ids)::uuid[]) AND NOT cw.stale
ORDER BY cw.video_id,cw.start_ts;
