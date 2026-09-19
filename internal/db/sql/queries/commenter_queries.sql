-- UpsertCommentersForVideo inserts/updates commenters from a video's comments.
-- Identity is comment_author_id(author_id, author_url); rows with empty identity are skipped.
-- name: UpsertCommentersForVideo :exec
INSERT INTO commenters (
    source,
    author_id,
    author_url,
    display_name,
    first_seen,
    last_seen,
    comment_count,
    updated_at
)
SELECT
    vc.source,
    comment_author_id(vc.author_id, vc.author_url) AS author_id,
    COALESCE(MAX(NULLIF(btrim(COALESCE(vc.author_url, '')), '')), '') AS author_url,
    COALESCE(MAX(NULLIF(btrim(COALESCE(vc.author, '')), '')), '') AS display_name,
    MIN(COALESCE(vc.published_at, vc.created_at)) AS first_seen,
    MAX(COALESCE(vc.published_at, vc.created_at)) AS last_seen,
    0 AS comment_count,
    NOW() AS updated_at
FROM video_comments vc
WHERE vc.video_id = sqlc.arg(video_id)
  AND comment_author_id(vc.author_id, vc.author_url) IS NOT NULL
GROUP BY vc.source, comment_author_id(vc.author_id, vc.author_url)
ON CONFLICT (source, author_id)
DO UPDATE SET
    author_url = CASE
        WHEN EXCLUDED.author_url <> '' THEN EXCLUDED.author_url
        ELSE commenters.author_url
    END,
    display_name = CASE
        WHEN EXCLUDED.display_name <> '' THEN EXCLUDED.display_name
        ELSE commenters.display_name
    END,
    first_seen = LEAST(commenters.first_seen, EXCLUDED.first_seen),
    last_seen = GREATEST(commenters.last_seen, EXCLUDED.last_seen),
    updated_at = NOW();

-- AttachCommentersForVideo sets video_comments.commenter_id from matching commenters.
-- name: AttachCommentersForVideo :execrows
UPDATE video_comments vc
SET commenter_id = c.id,
    updated_at = NOW()
FROM commenters c
WHERE vc.video_id = sqlc.arg(video_id)
  AND vc.commenter_id IS NULL
  AND c.source = vc.source
  AND c.author_id = comment_author_id(vc.author_id, vc.author_url);

-- RefreshCommenterStatsForVideo recounts comment_count/first_seen/last_seen for
-- commenters that appear on this video (across all their comments).
-- name: RefreshCommenterStatsForVideo :exec
UPDATE commenters c
SET comment_count = s.comment_count,
    first_seen = s.first_seen,
    last_seen = s.last_seen,
    updated_at = NOW()
FROM (
    SELECT
        vc.commenter_id,
        COUNT(*)::bigint AS comment_count,
        MIN(COALESCE(vc.published_at, vc.created_at)) AS first_seen,
        MAX(COALESCE(vc.published_at, vc.created_at)) AS last_seen
    FROM video_comments vc
    WHERE vc.commenter_id IN (
        SELECT DISTINCT vc2.commenter_id
        FROM video_comments vc2
        WHERE vc2.video_id = sqlc.arg(video_id)
          AND vc2.commenter_id IS NOT NULL
    )
    GROUP BY vc.commenter_id
) s
WHERE c.id = s.commenter_id;

-- UpsertCommenterNamesForVideo upserts name history for commenters on this video.
-- n is the full grouped count across all of each commenter's comments.
-- name: UpsertCommenterNamesForVideo :exec
INSERT INTO commenter_names (commenter_id, display_name, first_seen, last_seen, n)
SELECT
    vc.commenter_id,
    btrim(vc.author) AS display_name,
    MIN(COALESCE(vc.published_at, vc.created_at)) AS first_seen,
    MAX(COALESCE(vc.published_at, vc.created_at)) AS last_seen,
    COUNT(*)::bigint AS n
FROM video_comments vc
WHERE vc.commenter_id IN (
    SELECT DISTINCT vc2.commenter_id
    FROM video_comments vc2
    WHERE vc2.video_id = sqlc.arg(video_id)
      AND vc2.commenter_id IS NOT NULL
)
  AND vc.author IS NOT NULL
  AND btrim(vc.author) <> ''
GROUP BY vc.commenter_id, btrim(vc.author)
ON CONFLICT (commenter_id, display_name)
DO UPDATE SET
    first_seen = LEAST(commenter_names.first_seen, EXCLUDED.first_seen),
    last_seen = GREATEST(commenter_names.last_seen, EXCLUDED.last_seen),
    n = EXCLUDED.n;

-- LinkCommentersToChannelsForVideo sets commenters.channel_id when an archived
-- channel matches author_id or canonical_url (www/trailing slash ignored).
-- name: LinkCommentersToChannelsForVideo :exec
UPDATE commenters c
SET channel_id = ch.id,
    updated_at = NOW()
FROM channels ch
WHERE c.channel_id IS NULL
  AND c.id IN (
      SELECT DISTINCT vc.commenter_id
      FROM video_comments vc
      WHERE vc.video_id = sqlc.arg(video_id)
        AND vc.commenter_id IS NOT NULL
  )
  AND (
      (ch.channel_id <> '' AND ch.channel_id = c.author_id)
      OR (
          c.author_url <> ''
          AND regexp_replace(lower(regexp_replace(ch.canonical_url, '^https?://(www\.)?', '')), '/$', '')
            = regexp_replace(lower(regexp_replace(c.author_url, '^https?://(www\.)?', '')), '/$', '')
      )
  );

-- ListVideosNeedingCommenters returns distinct videos with unlinked comments.
-- name: ListVideosNeedingCommenters :many
SELECT DISTINCT video_id
FROM video_comments
WHERE commenter_id IS NULL
  AND author_id IS NOT NULL
  AND btrim(author_id) <> ''
LIMIT sqlc.arg(batch_size)::int;

-- name: GetCommenter :one
SELECT * FROM commenters WHERE id = sqlc.arg(id);

-- name: GetCommenterByIdentity :one
SELECT * FROM commenters
WHERE source = sqlc.arg(source)
  AND author_id = sqlc.arg(author_id);

-- SearchCommenters matches display_name / author_id / author_url via ILIKE + trgm.
-- name: SearchCommenters :many
SELECT
    c.*,
    GREATEST(
        similarity(c.display_name, sqlc.arg(query)),
        similarity(c.author_id, sqlc.arg(query)),
        similarity(c.author_url, sqlc.arg(query))
    )::float4 AS score
FROM commenters c
WHERE btrim(sqlc.arg(query)::text) <> ''
  AND (
    c.display_name ILIKE '%' || sqlc.arg(query) || '%'
    OR c.author_id ILIKE '%' || sqlc.arg(query) || '%'
    OR c.author_url ILIKE '%' || sqlc.arg(query) || '%'
    OR similarity(c.display_name, sqlc.arg(query)) > 0.3
  )
ORDER BY score DESC, c.comment_count DESC, c.display_name
LIMIT sqlc.arg(page_size)::int
OFFSET sqlc.arg(page_offset)::int;

-- name: ListCommenterNames :many
SELECT * FROM commenter_names
WHERE commenter_id = sqlc.arg(commenter_id)
ORDER BY n DESC, last_seen DESC;

-- ListCommenterComments returns paginated comments for a commenter with video title.
-- name: ListCommenterComments :many
SELECT
    c.id,
    c.video_id,
    c.source,
    c.comment_id,
    c.parent_id,
    c.author,
    c.author_id,
    c.author_url,
    c.published_at,
    c.like_count,
    c.text,
    c.created_at,
    c.commenter_id,
    v.title AS video_title
FROM video_comments c
JOIN videos v ON v.id = c.video_id
WHERE c.commenter_id = sqlc.arg(commenter_id)
ORDER BY c.published_at DESC NULLS LAST, c.created_at DESC
LIMIT sqlc.arg(page_size)::int
OFFSET sqlc.arg(page_offset)::int;

-- name: ListWatchlist :many
SELECT
    w.user_id,
    w.commenter_id,
    w.note,
    w.created_at,
    c.source,
    c.author_id,
    c.author_url,
    c.display_name,
    c.comment_count,
    c.last_seen
FROM commenter_watchlist w
JOIN commenters c ON c.id = w.commenter_id
WHERE w.user_id = sqlc.arg(user_id)
ORDER BY w.created_at DESC;

-- name: WatchCommenter :exec
INSERT INTO commenter_watchlist (user_id, commenter_id, note)
VALUES (sqlc.arg(user_id), sqlc.arg(commenter_id), COALESCE(sqlc.narg(note), ''))
ON CONFLICT (user_id, commenter_id)
DO UPDATE SET note = EXCLUDED.note;

-- name: UnwatchCommenter :exec
DELETE FROM commenter_watchlist
WHERE user_id = sqlc.arg(user_id)
  AND commenter_id = sqlc.arg(commenter_id);

-- name: UpsertOsintFlag :one
INSERT INTO osint_flags (kind, commenter_id, video_id, campaign_id, score, evidence, natural_key)
VALUES (
    sqlc.arg(kind),
    sqlc.narg(commenter_id),
    sqlc.narg(video_id),
    sqlc.narg(campaign_id),
    COALESCE(sqlc.narg(score), 0),
    COALESCE(sqlc.arg(evidence)::jsonb, '{}'::jsonb),
    sqlc.narg(natural_key)
)
ON CONFLICT (natural_key) WHERE natural_key IS NOT NULL
DO UPDATE SET
    score = EXCLUDED.score,
    evidence = EXCLUDED.evidence
RETURNING *;

-- name: DismissOsintFlag :exec
UPDATE osint_flags
SET dismissed_at = NOW(),
    dismissed_by = sqlc.arg(dismissed_by)
WHERE id = sqlc.arg(id)
  AND dismissed_at IS NULL;

-- name: ListOpenOsintFlags :many
SELECT * FROM osint_flags
WHERE dismissed_at IS NULL
ORDER BY created_at DESC
LIMIT sqlc.arg(page_size)::int
OFFSET sqlc.arg(page_offset)::int;

-- name: UpsertCampaign :one
INSERT INTO campaigns (
    kind,
    simhash,
    normalized_text,
    first_seen,
    last_seen,
    comment_count,
    commenter_count,
    video_count,
    evidence
) VALUES (
    sqlc.arg(kind),
    sqlc.narg(simhash),
    COALESCE(sqlc.narg(normalized_text), ''),
    COALESCE(sqlc.narg(first_seen), NOW()),
    COALESCE(sqlc.narg(last_seen), NOW()),
    COALESCE(sqlc.narg(comment_count), 0),
    COALESCE(sqlc.narg(commenter_count), 0),
    COALESCE(sqlc.narg(video_count), 0),
    COALESCE(sqlc.arg(evidence)::jsonb, '{}'::jsonb)
)
ON CONFLICT (simhash)
WHERE kind = 'copypaste' AND simhash IS NOT NULL
DO UPDATE SET
    normalized_text = CASE
        WHEN EXCLUDED.normalized_text <> '' THEN EXCLUDED.normalized_text
        ELSE campaigns.normalized_text
    END,
    first_seen = LEAST(campaigns.first_seen, EXCLUDED.first_seen),
    last_seen = GREATEST(campaigns.last_seen, EXCLUDED.last_seen),
    comment_count = GREATEST(campaigns.comment_count, EXCLUDED.comment_count),
    commenter_count = GREATEST(campaigns.commenter_count, EXCLUDED.commenter_count),
    video_count = GREATEST(campaigns.video_count, EXCLUDED.video_count),
    evidence = EXCLUDED.evidence,
    updated_at = NOW()
RETURNING *;

-- name: GetCampaign :one
SELECT * FROM campaigns WHERE id = sqlc.arg(id);

-- name: ListCampaigns :many
SELECT * FROM campaigns
ORDER BY last_seen DESC, created_at DESC
LIMIT sqlc.arg(page_size)::int
OFFSET sqlc.arg(page_offset)::int;

-- name: UpsertCampaignMember :exec
INSERT INTO campaign_members (campaign_id, comment_id)
VALUES (sqlc.arg(campaign_id), sqlc.arg(comment_id))
ON CONFLICT (campaign_id, comment_id) DO NOTHING;

-- name: UpsertCommenterLink :exec
INSERT INTO commenter_links (a_id, b_id, kind, score, evidence, created_by)
VALUES (
    LEAST(sqlc.arg(a_id)::uuid, sqlc.arg(b_id)::uuid),
    GREATEST(sqlc.arg(a_id)::uuid, sqlc.arg(b_id)::uuid),
    sqlc.arg(kind),
    COALESCE(sqlc.narg(score), 0),
    COALESCE(sqlc.arg(evidence)::jsonb, '{}'::jsonb),
    sqlc.narg(created_by)
)
ON CONFLICT (a_id, b_id, kind)
DO UPDATE SET
    score = EXCLUDED.score,
    evidence = EXCLUDED.evidence,
    created_by = COALESCE(EXCLUDED.created_by, commenter_links.created_by);

-- name: ListCommenterLinks :many
SELECT * FROM commenter_links
WHERE a_id = sqlc.arg(commenter_id) OR b_id = sqlc.arg(commenter_id)
ORDER BY created_at DESC;

-- name: UpsertCommentScore :exec
INSERT INTO comment_scores (comment_id, model_digest, sentiment, toxicity, labels, simhash, scored_at)
VALUES (
    sqlc.arg(comment_id),
    COALESCE(sqlc.narg(model_digest), ''),
    sqlc.narg(sentiment),
    sqlc.narg(toxicity),
    COALESCE(sqlc.arg(labels)::jsonb, '{}'::jsonb),
    sqlc.narg(simhash),
    NOW()
)
ON CONFLICT (comment_id)
DO UPDATE SET
    model_digest = EXCLUDED.model_digest,
    sentiment = EXCLUDED.sentiment,
    toxicity = EXCLUDED.toxicity,
    labels = EXCLUDED.labels,
    simhash = EXCLUDED.simhash,
    scored_at = NOW();

-- name: ListUnscoredCommentsForVideo :many
SELECT c.id, c.video_id, c.source, c.comment_id, c.author, c.author_id, c.text, c.like_count, c.published_at
FROM video_comments c
LEFT JOIN comment_scores s ON s.comment_id = c.id
WHERE c.video_id = sqlc.arg(video_id)
  AND s.comment_id IS NULL
ORDER BY c.published_at DESC NULLS LAST, c.id
LIMIT sqlc.arg(page_limit)::int;

-- CommentClassifyInputHash fingerprints unscored-input churn for a video.
-- name: CommentClassifyInputHash :one
SELECT (COALESCE(MAX(c.updated_at), 'epoch'::timestamptz)::text || COUNT(*)::text)::text AS input_hash
FROM video_comments c
WHERE c.video_id = sqlc.arg(video_id);

-- name: UpsertSpeechScore :one
INSERT INTO speech_scores (
    video_id, set_id, start_ts, end_ts, model_digest, sentiment, toxicity, labels, scored_at
) VALUES (
    sqlc.arg(video_id),
    sqlc.narg(set_id),
    sqlc.arg(start_ts),
    sqlc.arg(end_ts),
    COALESCE(sqlc.narg(model_digest), ''),
    sqlc.narg(sentiment),
    sqlc.narg(toxicity),
    COALESCE(sqlc.arg(labels)::jsonb, '{}'::jsonb),
    NOW()
)
RETURNING *;

-- name: ListSpeechScoresForVideo :many
SELECT * FROM speech_scores
WHERE video_id = sqlc.arg(video_id)
ORDER BY start_ts, end_ts, scored_at DESC;

-- name: ListCommenterEdges :many
SELECT * FROM commenter_edges
WHERE commenter_id = sqlc.arg(commenter_id)
ORDER BY updated_at DESC, created_at DESC;

-- name: UpsertCommenterEdge :exec
INSERT INTO commenter_edges (from_channel_id, commenter_id, kind, weight, evidence, video_id)
VALUES (
    sqlc.narg(from_channel_id),
    sqlc.arg(commenter_id),
    sqlc.arg(kind),
    COALESCE(sqlc.narg(weight), 1),
    COALESCE(sqlc.arg(evidence)::jsonb, '{}'::jsonb),
    sqlc.narg(video_id)
)
ON CONFLICT (COALESCE(from_channel_id::text, ''), commenter_id, kind)
DO UPDATE SET
    weight = EXCLUDED.weight,
    evidence = EXCLUDED.evidence,
    video_id = COALESCE(EXCLUDED.video_id, commenter_edges.video_id),
    updated_at = NOW();

-- GetVideoCommentToneRollup averages scored sentiment/toxicity for a video.
-- name: GetVideoCommentToneRollup :one
SELECT
    COUNT(s.comment_id)::bigint AS scored_count,
    COALESCE(AVG(s.sentiment), 0)::float8 AS avg_sentiment,
    COALESCE(AVG(s.toxicity), 0)::float8 AS avg_toxicity
FROM video_comments c
JOIN comment_scores s ON s.comment_id = c.id
WHERE c.video_id = sqlc.arg(video_id);
