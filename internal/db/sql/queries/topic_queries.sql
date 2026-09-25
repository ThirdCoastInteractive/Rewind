-- name: UpsertTopic :one
INSERT INTO topics (tenant_id, slug, title, origin)
VALUES (sqlc.arg(tenant_id), sqlc.arg(slug), sqlc.arg(title), sqlc.arg(origin))
ON CONFLICT (tenant_id, slug) DO UPDATE SET
    title = CASE WHEN topics.origin = 'wiki' THEN topics.title ELSE EXCLUDED.title END,
    origin = CASE
        WHEN topics.origin = 'wiki' THEN topics.origin
        WHEN EXCLUDED.origin = 'wiki' THEN EXCLUDED.origin
        ELSE topics.origin
    END
RETURNING *;

-- name: GetTopic :one
SELECT * FROM topics WHERE tenant_id = sqlc.arg(tenant_id) AND slug = sqlc.arg(slug);

-- name: ListTopics :many
SELECT slug, title, origin FROM topics WHERE tenant_id = sqlc.arg(tenant_id)
ORDER BY title, slug
LIMIT sqlc.arg(page_limit);

-- name: ListTopicTenants :many
SELECT DISTINCT tenant_id FROM videos ORDER BY tenant_id;

-- name: InsertTopicAlias :exec
INSERT INTO topic_aliases (tenant_id, alias_norm, topic_slug, raw, source)
VALUES (sqlc.arg(tenant_id), sqlc.arg(alias_norm), sqlc.arg(topic_slug), sqlc.arg(raw), sqlc.arg(source))
ON CONFLICT (tenant_id, alias_norm) DO NOTHING;

-- name: GetTopicByAlias :one
SELECT t.slug, t.title, t.origin
FROM topic_aliases a
JOIN topics t ON t.tenant_id = a.tenant_id AND t.slug = a.topic_slug
WHERE a.tenant_id = sqlc.arg(tenant_id) AND a.alias_norm = sqlc.arg(alias_norm);

-- name: BindWindowTopic :exec
INSERT INTO context_window_topics (tenant_id, window_id, topic_slug, raw, match_kind)
VALUES (sqlc.arg(tenant_id), sqlc.arg(window_id), sqlc.arg(topic_slug), sqlc.arg(raw), sqlc.arg(match_kind))
ON CONFLICT (tenant_id, window_id, topic_slug) DO UPDATE SET
    raw = EXCLUDED.raw,
    match_kind = EXCLUDED.match_kind;

-- name: DeleteWindowTopics :exec
DELETE FROM context_window_topics WHERE tenant_id = sqlc.arg(tenant_id) AND window_id = sqlc.arg(window_id);

-- name: MarkWindowTopicsResolved :exec
UPDATE context_windows SET topic_resolved_at = NOW() WHERE id = sqlc.arg(id);

-- name: ListWindowsForTopicBind :many
SELECT context_windows.id, context_windows.video_id, context_windows.title, context_windows.topics, context_windows.entities, context_windows.kind
FROM context_windows
JOIN videos ON videos.id = context_windows.video_id
WHERE NOT stale AND kind = 'window' AND topic_resolved_at IS NULL
  AND videos.tenant_id = sqlc.arg(tenant_id)
ORDER BY context_windows.updated_at DESC, context_windows.id
LIMIT sqlc.arg(page_limit);

-- name: ListTopicWindows :many
SELECT cw.id, cw.video_id, cw.start_ts, cw.end_ts, cw.title, cw.summary,
       v.title AS video_title, v.uploader, v.channel_row_id,
       cwt.match_kind, cwt.raw, t.slug AS topic_slug, t.title AS topic_title
FROM context_window_topics cwt
JOIN context_windows cw ON cw.id = cwt.window_id
JOIN videos v ON v.id = cw.video_id AND v.tenant_id = cwt.tenant_id
JOIN topics t ON t.tenant_id = cwt.tenant_id AND t.slug = cwt.topic_slug
WHERE cwt.topic_slug = sqlc.arg(slug) AND cwt.tenant_id = sqlc.arg(tenant_id)
  AND NOT cw.stale
  AND cw.kind = 'window'
ORDER BY cw.updated_at DESC, cw.id
LIMIT sqlc.arg(page_limit);

-- name: CountTopicWindows :one
SELECT count(*)::bigint
FROM context_window_topics cwt
JOIN context_windows cw ON cw.id = cwt.window_id
JOIN videos v ON v.id = cw.video_id AND v.tenant_id = cwt.tenant_id
WHERE cwt.topic_slug = sqlc.arg(slug) AND cwt.tenant_id = sqlc.arg(tenant_id) AND NOT cw.stale AND cw.kind = 'window';

-- name: CountTopicChannels :one
SELECT count(DISTINCT v.channel_row_id)::bigint
FROM context_window_topics cwt
JOIN context_windows cw ON cw.id = cwt.window_id
JOIN videos v ON v.id = cw.video_id AND v.tenant_id = cwt.tenant_id
WHERE cwt.topic_slug = sqlc.arg(slug) AND cwt.tenant_id = sqlc.arg(tenant_id)
  AND NOT cw.stale
  AND cw.kind = 'window'
  AND v.channel_row_id IS NOT NULL;

-- name: ListRelatedTopics :many
SELECT t.slug, t.title, count(*)::bigint AS n
FROM context_window_topics a
JOIN context_window_topics b ON a.window_id = b.window_id AND a.topic_slug <> b.topic_slug
  AND b.tenant_id = a.tenant_id
JOIN topics t ON t.tenant_id = b.tenant_id AND t.slug = b.topic_slug
JOIN context_windows cw ON cw.id = a.window_id AND NOT cw.stale AND cw.kind = 'window'
JOIN videos v ON v.id = cw.video_id AND v.tenant_id = a.tenant_id
WHERE a.tenant_id = sqlc.arg(tenant_id) AND a.topic_slug = sqlc.arg(slug)
GROUP BY t.slug, t.title
ORDER BY n DESC, t.title
LIMIT 12;

-- name: ListWindowTopicBinds :many
SELECT cwt.window_id, t.slug, t.title, cwt.match_kind
FROM context_window_topics cwt
JOIN topics t ON t.tenant_id = cwt.tenant_id AND t.slug = cwt.topic_slug
JOIN context_windows cw ON cw.id = cwt.window_id AND NOT cw.stale
JOIN videos v ON v.id = cw.video_id
WHERE cwt.window_id = ANY(sqlc.arg(window_ids)::uuid[]) AND cwt.tenant_id = v.tenant_id
ORDER BY t.title, cwt.window_id;

-- name: ListTopicNeighborsForVideo :many
SELECT cw.id, cw.video_id, cw.start_ts, cw.end_ts, cw.title,
       v.title AS video_title, v.uploader, t.slug AS topic_slug, t.title AS topic_title
FROM context_windows mine
JOIN context_window_topics mt ON mt.window_id = mine.id
JOIN context_window_topics ot ON ot.topic_slug = mt.topic_slug AND ot.tenant_id = mt.tenant_id AND ot.window_id <> mine.id
JOIN context_windows cw ON cw.id = ot.window_id AND NOT cw.stale AND cw.kind = 'window'
JOIN videos v ON v.id = cw.video_id AND v.tenant_id = ot.tenant_id
JOIN videos mv ON mv.id = mine.video_id AND mv.tenant_id = mt.tenant_id
JOIN topics t ON t.tenant_id = ot.tenant_id AND t.slug = ot.topic_slug
WHERE mine.video_id = sqlc.arg(video_id)
  AND NOT mine.stale
  AND v.id <> mine.video_id
  AND v.channel_row_id IS DISTINCT FROM mv.channel_row_id
ORDER BY cw.updated_at DESC, cw.id
LIMIT sqlc.arg(page_limit);

-- name: ListTopicsNeedingStub :many
SELECT t.slug, t.title
FROM topics t
WHERE t.tenant_id = sqlc.arg(tenant_id) AND t.origin <> 'wiki'
  AND NOT EXISTS (
    SELECT 1 FROM wiki_pages w WHERE w.tenant_id = t.tenant_id AND w.tree = 'topic' AND w.slug = t.slug
  )
  AND (
    SELECT count(DISTINCT v.channel_row_id)
    FROM context_window_topics cwt
    JOIN context_windows cw ON cw.id = cwt.window_id
    JOIN videos v ON v.id = cw.video_id AND v.tenant_id = cwt.tenant_id
    WHERE cwt.tenant_id = t.tenant_id AND cwt.topic_slug = t.slug AND NOT cw.stale AND cw.kind = 'window' AND v.channel_row_id IS NOT NULL
  ) >= 3
ORDER BY t.slug;

-- name: SearchTopicWindows :many
SELECT cw.id, cw.video_id, cw.start_ts, cw.end_ts, cw.title, cw.summary,
       v.title AS video_title, v.uploader, v.channel_row_id,
       cwt.match_kind, cwt.raw, t.slug AS topic_slug, t.title AS topic_title
FROM context_window_topics cwt
JOIN context_windows cw ON cw.id = cwt.window_id
JOIN videos v ON v.id = cw.video_id AND v.tenant_id = cwt.tenant_id
JOIN topics t ON t.tenant_id = cwt.tenant_id AND t.slug = cwt.topic_slug
WHERE cwt.tenant_id = sqlc.arg(tenant_id) AND cwt.tenant_id = v.tenant_id AND NOT cw.stale AND cw.kind = 'window'
  AND (
    t.slug = sqlc.arg(query)
    OR t.title ILIKE '%' || sqlc.arg(query) || '%'
    OR EXISTS (
      SELECT 1 FROM topic_aliases a
      WHERE a.tenant_id = cwt.tenant_id AND a.topic_slug = t.slug AND (a.alias_norm = sqlc.arg(alias_norm) OR a.raw ILIKE '%' || sqlc.arg(query) || '%')
    )
  )
ORDER BY cw.updated_at DESC, cw.id
LIMIT sqlc.arg(page_limit);
