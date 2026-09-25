-- name: ListNetworkChannels :many
SELECT ch.*, COALESCE(cr.name, '')::text AS creator_name,
 (SELECT count(*) FROM videos v WHERE v.channel_row_id=ch.id)::bigint AS video_count
FROM channels ch LEFT JOIN creators cr ON cr.id=ch.creator_id
ORDER BY ch.uploader, ch.id;

-- name: GetNetworkXChannel :one
SELECT * FROM channels
WHERE platform IN ('twitter','x') AND (
 lower(ltrim(identity_key,'@'))=sqlc.arg(handle)
 OR lower(substring(canonical_url from '^https?://(?:www\.)?(?:x|twitter)\.com/([^/?#]+)'))=sqlc.arg(handle)
)
ORDER BY created_at,id LIMIT 1;

-- name: ListNetworkContext :many
WITH own_topics AS (
 SELECT DISTINCT cwt.topic_slug AS topic
 FROM context_window_topics cwt
 JOIN context_windows cw ON cw.id = cwt.window_id
 JOIN videos v ON v.id = cw.video_id
 WHERE v.channel_row_id = ANY(sqlc.arg(channel_ids)::uuid[]) AND NOT cw.stale AND cw.kind = 'window'
), candidates AS (
 SELECT cw.id, cw.video_id, cw.start_ts, cw.end_ts, cw.title, cw.summary,
 cw.topics, cw.entities, v.title AS video_title, v.uploader, cw.updated_at,
 (v.channel_row_id = ANY(sqlc.arg(channel_ids)::uuid[]))::boolean AS selected_channel,
 ARRAY(
   SELECT t.title FROM context_window_topics x
   JOIN topics t ON t.slug = x.topic_slug
   WHERE x.window_id = cw.id AND x.topic_slug IN (SELECT topic FROM own_topics)
 )::text[] AS shared_topics
 FROM context_windows cw JOIN videos v ON v.id = cw.video_id
 WHERE NOT cw.stale AND cw.kind = 'window'
 AND (v.channel_row_id = ANY(sqlc.arg(channel_ids)::uuid[])
   OR EXISTS (
     SELECT 1 FROM context_window_topics x
     WHERE x.window_id = cw.id AND x.topic_slug IN (SELECT topic FROM own_topics)
   ))
 AND (sqlc.arg(query)::text = '' OR concat_ws(' ', cw.title, cw.summary, array_to_string(cw.topics, ' '), array_to_string(cw.entities, ' ')) ILIKE '%' || sqlc.arg(query) || '%')
), ranked AS (
 SELECT *, row_number() OVER (PARTITION BY selected_channel ORDER BY cardinality(shared_topics) DESC, updated_at DESC, id) AS position
 FROM candidates WHERE selected_channel OR cardinality(shared_topics) >= 1
)
SELECT id, video_id, start_ts, end_ts, title, summary, topics, entities, video_title, uploader, selected_channel, shared_topics
FROM ranked WHERE position <= 15
ORDER BY selected_channel DESC, position;

-- Sparse commenter nodes for /network (watchlisted, open flag, user link, or channel_id).
-- name: ListCommenterNetworkNodes :many
WITH sparse AS (
  SELECT c.id FROM commenters c WHERE c.channel_id IS NOT NULL
  UNION
  SELECT w.commenter_id FROM commenter_watchlist w
  UNION
  SELECT f.commenter_id FROM osint_flags f
  WHERE f.dismissed_at IS NULL AND f.commenter_id IS NOT NULL
  UNION
  SELECT l.a_id FROM commenter_links l WHERE l.kind = 'user'
  UNION
  SELECT l.b_id FROM commenter_links l WHERE l.kind = 'user'
)
SELECT c.id,
       c.source,
       c.display_name,
       c.author_url,
       c.comment_count,
       c.channel_id,
       EXISTS (
         SELECT 1 FROM commenter_watchlist w
         WHERE w.commenter_id = c.id AND w.user_id = sqlc.arg(user_id)
       ) AS watchlisted,
       COALESCE((
         SELECT array_agg(DISTINCT f.kind ORDER BY f.kind)
         FROM osint_flags f
         WHERE f.commenter_id = c.id AND f.dismissed_at IS NULL
       ), '{}'::text[])::text[] AS open_flag_kinds
FROM commenters c
WHERE c.id IN (SELECT id FROM sparse)
ORDER BY c.comment_count DESC, c.display_name, c.id
LIMIT 200;

-- Pre-aggregated commenter edges (strongest-N).
-- name: ListCommenterNetworkEdges :many
WITH sparse AS (
  SELECT c.id FROM commenters c WHERE c.channel_id IS NOT NULL
  UNION
  SELECT w.commenter_id FROM commenter_watchlist w
  UNION
  SELECT f.commenter_id FROM osint_flags f
  WHERE f.dismissed_at IS NULL AND f.commenter_id IS NOT NULL
  UNION
  SELECT l.a_id FROM commenter_links l WHERE l.kind = 'user'
  UNION
  SELECT l.b_id FROM commenter_links l WHERE l.kind = 'user'
), edge_rows AS (
  SELECT e.id::text AS id,
         e.from_channel_id,
         e.commenter_id,
         NULL::uuid AS peer_commenter_id,
         e.kind,
         e.weight::float8 AS weight,
         COALESCE(e.evidence->>'summary', e.kind)::text AS evidence,
         e.video_id
  FROM commenter_edges e
  WHERE e.commenter_id IN (SELECT id FROM sparse)
    AND (e.kind <> 'commented_by' OR e.from_channel_id IS NOT NULL)
  UNION ALL
  SELECT ('style:' || l.a_id::text || ':' || l.b_id::text) AS id,
         NULL::uuid AS from_channel_id,
         l.a_id AS commenter_id,
         l.b_id AS peer_commenter_id,
         'style'::text AS kind,
         GREATEST(l.score, 1)::float8 AS weight,
         COALESCE(l.evidence->>'summary', 'style suggestion')::text AS evidence,
         NULL::uuid AS video_id
  FROM commenter_links l
  WHERE l.kind = 'style'
    AND l.a_id IN (SELECT id FROM sparse)
    AND l.b_id IN (SELECT id FROM sparse)
)
SELECT id, from_channel_id, commenter_id, peer_commenter_id, kind, weight, evidence, video_id
FROM edge_rows
ORDER BY weight DESC, id
LIMIT 200;
