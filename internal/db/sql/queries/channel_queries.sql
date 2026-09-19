-- ListChannels aggregates the library by uploader: per-channel video counts,
-- totals, a representative latest archived video that has a thumbnail, the channel URL
-- (from the generated channel_url/uploader_url columns — never the info
-- JSONB, which is far too heavy to touch per row), and whether a channel
-- watch already covers it. Optional name filter for the list page search box.
-- name: ListChannels :many
WITH agg AS (
    SELECT
        v.uploader,
        COUNT(*) AS video_count,
        COALESCE(SUM(v.duration_seconds), 0)::bigint AS total_duration_seconds,
        COALESCE(SUM(v.file_size), 0)::bigint AS total_size_bytes,
        MAX(v.upload_date)::date AS latest_upload,
        (COALESCE(MAX(NULLIF(v.channel_url, '')), MAX(NULLIF(v.uploader_url, '')), ''))::text AS channel_url,
        (COALESCE(MAX(NULLIF(v.uploader_url, '')), ''))::text AS uploader_url
    FROM videos v
    WHERE btrim(v.uploader) <> ''
      AND (sqlc.narg('filter')::text IS NULL OR v.uploader ILIKE '%' || sqlc.narg('filter') || '%')
    GROUP BY v.uploader
)
SELECT
    agg.*,
    (SELECT v2.id FROM videos v2
     WHERE v2.uploader = agg.uploader
       AND v2.media = 'file'
       AND COALESCE(v2.thumbnail_path, '') <> ''
     ORDER BY v2.upload_date DESC NULLS LAST, v2.created_at DESC
     LIMIT 1) AS latest_video_id,
    (w.id IS NOT NULL)::boolean AS watched
FROM agg
LEFT JOIN LATERAL (
    SELECT wc.id FROM watched_channels wc
    WHERE (agg.channel_url <> '' AND wc.url = agg.channel_url)
       OR (agg.uploader_url <> '' AND wc.url = agg.uploader_url)
       OR wc.label = agg.uploader
    LIMIT 1
) w ON TRUE
ORDER BY agg.video_count DESC, agg.uploader;

-- GetChannelOverview returns the aggregate stats for one uploader, plus the
-- id of a matching channel watch when one exists.
-- name: GetChannelOverview :one
WITH agg AS (
    SELECT
        v.uploader,
        COUNT(*) AS video_count,
        COALESCE(SUM(v.duration_seconds), 0)::bigint AS total_duration_seconds,
        COALESCE(SUM(v.file_size), 0)::bigint AS total_size_bytes,
        COALESCE(SUM(v.view_count), 0)::bigint AS total_views,
        MAX(v.upload_date)::date AS latest_upload,
        (COALESCE(MAX(NULLIF(v.channel_url, '')), MAX(NULLIF(v.uploader_url, '')), ''))::text AS channel_url,
        (COALESCE(MAX(NULLIF(v.uploader_url, '')), ''))::text AS uploader_url
    FROM videos v
    WHERE v.uploader = sqlc.arg(uploader)
    GROUP BY v.uploader
)
SELECT agg.*, w.id AS watch_id
FROM agg
LEFT JOIN LATERAL (
    SELECT wc.id FROM watched_channels wc
    WHERE (agg.channel_url <> '' AND wc.url = agg.channel_url)
       OR (agg.uploader_url <> '' AND wc.url = agg.uploader_url)
       OR wc.label = agg.uploader
    LIMIT 1
) w ON TRUE;

-- UpsertChannel inserts or updates a first-class channel row keyed by
-- platform + identity_key (channel_id / uploader_id / canonical URL).
-- name: UpsertChannel :one
INSERT INTO channels (
    platform,
    identity_key,
    channel_id,
    uploader,
    canonical_url,
    search
) VALUES (
    sqlc.arg(platform),
    sqlc.arg(identity_key),
    sqlc.arg(channel_id),
    sqlc.arg(uploader),
    sqlc.arg(canonical_url),
    setweight(to_tsvector('simple', coalesce(sqlc.arg(uploader), '')), 'A')
)
ON CONFLICT (platform, identity_key)
DO UPDATE SET
    channel_id = CASE WHEN EXCLUDED.channel_id <> '' THEN EXCLUDED.channel_id ELSE channels.channel_id END,
    uploader = CASE WHEN EXCLUDED.uploader <> '' THEN EXCLUDED.uploader ELSE channels.uploader END,
    canonical_url = CASE WHEN EXCLUDED.canonical_url <> '' THEN EXCLUDED.canonical_url ELSE channels.canonical_url END,
    search = EXCLUDED.search,
    updated_at = NOW()
RETURNING *;

-- SetVideoChannelAndFormat attaches a video to its channel row and stores format.
-- name: SetVideoChannelAndFormat :exec
UPDATE videos
SET channel_row_id = sqlc.arg(channel_row_id),
    format = sqlc.arg(format),
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- GetChannel fetches one first-class channel row.
-- name: GetChannel :one
SELECT * FROM channels WHERE id = sqlc.arg(id);

-- GetChannelByIdentity looks up a channel by platform + identity_key.
-- name: GetChannelByIdentity :one
SELECT * FROM channels WHERE platform = sqlc.arg(platform) AND identity_key = sqlc.arg(identity_key);

-- GetChannelByChannelID looks up a channel by platform + native channel_id (UC…).
-- name: GetChannelByChannelID :one
SELECT * FROM channels
WHERE platform = sqlc.arg(platform)
  AND channel_id = sqlc.arg(channel_id)
  AND channel_id <> ''
LIMIT 1;

-- GetChannelByHandleGuess matches a @handle to an archived channel's identity or uploader.
-- name: GetChannelByHandleGuess :one
SELECT * FROM channels
WHERE platform = sqlc.arg(platform)
  AND (
    identity_key ILIKE sqlc.arg(handle)
    OR identity_key ILIKE '@' || sqlc.arg(handle)
    OR uploader ILIKE sqlc.arg(handle)
  )
ORDER BY
    CASE
        WHEN identity_key ILIKE '@' || sqlc.arg(handle) THEN 0
        WHEN identity_key ILIKE sqlc.arg(handle) THEN 1
        ELSE 2
    END
LIMIT 1;

-- GetChannelByTwitterHandle matches twitter.com / x.com profile URLs.
-- name: GetChannelByTwitterHandle :one
SELECT * FROM channels
WHERE canonical_url ~* ('^https?://(www\.)?(twitter|x)\.com/' || sqlc.arg(handle) || '/?$')
LIMIT 1;

-- RelabelTwitterMentionsMisfiledAsYouTube rewrites @mentions harvested as
-- youtube.com/@handle when the source channel is actually Twitter/X.
-- name: RelabelTwitterMentionsMisfiledAsYouTube :exec
UPDATE channel_edges e
SET
    to_url = 'https://x.com/' || substring(e.to_url from '(?i)youtube\.com/@([^/?#]+)'),
    to_channel_id = NULL,
    updated_at = NOW()
FROM channels c
WHERE e.from_channel_id = c.id
  AND e.kind = 'mention'
  AND e.to_url ~* 'youtube\.com/@'
  AND substring(e.to_url from '(?i)youtube\.com/@([^/?#]+)') IS NOT NULL
  AND (
    c.platform IN ('twitter', 'x')
    OR c.canonical_url ~* 'https?://(www\.)?(twitter|x)\.com/'
  )
  AND NOT EXISTS (
    SELECT 1 FROM channel_edges e2
    WHERE e2.from_channel_id = e.from_channel_id
      AND e2.kind = e.kind
      AND e2.id <> e.id
      AND COALESCE(e2.to_channel_id::text, e2.to_url)
        = 'https://x.com/' || substring(e.to_url from '(?i)youtube\.com/@([^/?#]+)')
  );

-- GetChannelByCanonicalURL matches a stored canonical URL, ignoring www / trailing slash.
-- name: GetChannelByCanonicalURL :one
SELECT * FROM channels
WHERE canonical_url <> '' AND (
    canonical_url = sqlc.arg(url)
    OR rtrim(replace(replace(lower(canonical_url), 'https://www.', 'https://'), 'http://www.', 'http://'), '/')
     = rtrim(replace(replace(lower(sqlc.arg(url)), 'https://www.', 'https://'), 'http://www.', 'http://'), '/')
)
LIMIT 1;

-- BumpChannelEdge increments an existing directed edge (matched by the unique
-- identity expression) and fills in a resolved to_channel_id / video_id when
-- those were previously null.
-- name: BumpChannelEdge :execrows
UPDATE channel_edges SET weight = weight + 1, evidence = sqlc.arg(evidence),
  to_channel_id = COALESCE(channel_edges.to_channel_id, sqlc.narg(to_channel_id)),
  video_id = COALESCE(channel_edges.video_id, sqlc.narg(video_id)),
  updated_at = NOW()
WHERE from_channel_id = sqlc.arg(from_channel_id)
  AND kind = sqlc.arg(kind)
  AND COALESCE(channel_edges.to_channel_id::text, channel_edges.to_url) = sqlc.arg(edge_key)::text;

-- InsertChannelEdge records a new directed channel relationship.
-- name: InsertChannelEdge :exec
INSERT INTO channel_edges (from_channel_id, to_channel_id, to_url, kind, evidence, video_id)
VALUES (sqlc.arg(from_channel_id), sqlc.narg(to_channel_id), sqlc.arg(to_url), sqlc.arg(kind), sqlc.arg(evidence), sqlc.narg(video_id));

-- ListChannelEdges returns the heaviest edges for the network page.
-- Unresolved to_url rows are included — they resolve once that channel is archived.
-- name: ListChannelEdges :many
SELECT e.*,
  fc.uploader AS from_uploader, fc.platform AS from_platform,
  fc.creator_id AS from_creator_id, COALESCE(fcr.name, '')::text AS from_creator_name,
  COALESCE(tc.uploader, '')::text AS to_uploader, COALESCE(tc.platform, '')::text AS to_platform,
  tc.creator_id AS to_creator_id, COALESCE(tcr.name, '')::text AS to_creator_name
FROM channel_edges e
JOIN channels fc ON fc.id = e.from_channel_id
LEFT JOIN creators fcr ON fcr.id = fc.creator_id
LEFT JOIN channels tc ON tc.id = e.to_channel_id
LEFT JOIN creators tcr ON tcr.id = tc.creator_id
ORDER BY (e.to_channel_id IS NOT NULL) DESC, e.weight DESC
LIMIT 500;

-- ListChannelEdgesForCreator returns edges that start or end on a creator's channels.
-- name: ListChannelEdgesForCreator :many
SELECT e.*,
  fc.uploader AS from_uploader, fc.platform AS from_platform,
  COALESCE(tc.uploader, '')::text AS to_uploader, COALESCE(tc.platform, '')::text AS to_platform
FROM channel_edges e
JOIN channels fc ON fc.id = e.from_channel_id
LEFT JOIN channels tc ON tc.id = e.to_channel_id
WHERE fc.creator_id = sqlc.arg(creator_id)
   OR tc.creator_id = sqlc.arg(creator_id)
ORDER BY e.weight DESC
LIMIT 200;

-- ListChannelEdgesForChannel returns edges that start or end at one channel.
-- name: ListChannelEdgesForChannel :many
SELECT e.*,
  fc.uploader AS from_uploader, fc.platform AS from_platform,
  COALESCE(tc.uploader, '')::text AS to_uploader, COALESCE(tc.platform, '')::text AS to_platform
FROM channel_edges e
JOIN channels fc ON fc.id = e.from_channel_id
LEFT JOIN channels tc ON tc.id = e.to_channel_id
WHERE e.from_channel_id = sqlc.arg(channel_id) OR e.to_channel_id = sqlc.arg(channel_id)
ORDER BY e.weight DESC;

-- ListChannelNeighborhood is the 1-hop graph around one channel, optional kind.
-- name: ListChannelNeighborhood :many
SELECT e.*,
  fc.uploader AS from_uploader, fc.platform AS from_platform,
  COALESCE(tc.uploader, '')::text AS to_uploader, COALESCE(tc.platform, '')::text AS to_platform,
  CASE WHEN e.from_channel_id = sqlc.arg(channel_id) THEN 'out' ELSE 'in' END::text AS direction
FROM channel_edges e
JOIN channels fc ON fc.id = e.from_channel_id
LEFT JOIN channels tc ON tc.id = e.to_channel_id
WHERE (e.from_channel_id = sqlc.arg(channel_id) OR e.to_channel_id = sqlc.arg(channel_id))
  AND (sqlc.narg('kind')::text IS NULL OR e.kind = sqlc.narg('kind'))
ORDER BY e.weight DESC
LIMIT sqlc.arg(page_limit);

-- ListChannelVideos is the slim MCP listing for one uploader or channel row.
-- name: ListChannelVideos :many
SELECT v.id, v.title, v.uploader, v.format, v.upload_date, v.duration_seconds,
       v.view_count, v.media, v.src
FROM videos v
WHERE (
        sqlc.narg('channel_row_id')::uuid IS NOT NULL
        AND v.channel_row_id = sqlc.narg('channel_row_id')
      )
   OR (
        sqlc.narg('channel_row_id')::uuid IS NULL
        AND sqlc.narg('uploader')::text IS NOT NULL
        AND v.uploader = sqlc.narg('uploader')
      )
ORDER BY v.upload_date DESC NULLS LAST, v.created_at DESC
LIMIT sqlc.arg(page_limit)
OFFSET sqlc.arg(page_offset);

-- ListChannelCatalog is titles + descriptions for MCP channel analysis.
-- name: ListChannelCatalog :many
SELECT v.id, v.title, v.uploader, v.format, v.upload_date, v.duration_seconds,
       v.view_count, v.media, v.src, v.description
FROM videos v
WHERE (
        sqlc.narg('channel_row_id')::uuid IS NOT NULL
        AND v.channel_row_id = sqlc.narg('channel_row_id')
      )
   OR (
        sqlc.narg('channel_row_id')::uuid IS NULL
        AND sqlc.narg('uploader')::text IS NOT NULL
        AND v.uploader = sqlc.narg('uploader')
      )
ORDER BY v.upload_date DESC NULLS LAST, v.created_at DESC
LIMIT sqlc.arg(page_limit)
OFFSET sqlc.arg(page_offset);

-- name: HasUnresolvedChannelEdges :one
SELECT EXISTS(
    SELECT 1 FROM channel_edges WHERE to_channel_id IS NULL AND to_url <> ''
)::boolean;

-- ResolveChannelEdges fills to_channel_id when an unresolved to_url now matches
-- an archived channel (canonical URL, UC id, @handle, or identity key).
-- Skip rows that would collide with an existing (from, kind, to_channel) identity.
-- name: ResolveChannelEdges :exec
UPDATE channel_edges e SET to_channel_id = c.id, updated_at = NOW()
FROM channels c
WHERE e.to_channel_id IS NULL AND e.to_url <> ''
AND NOT EXISTS (
    SELECT 1 FROM channel_edges e2
    WHERE e2.from_channel_id = e.from_channel_id
      AND e2.kind = e.kind
      AND e2.id <> e.id
      AND COALESCE(e2.to_channel_id::text, e2.to_url) = c.id::text
)
AND (
    c.canonical_url = e.to_url
    OR c.identity_key = e.to_url
    OR rtrim(replace(replace(lower(c.canonical_url), 'https://www.', 'https://'), 'http://www.', 'http://'), '/')
     = rtrim(replace(replace(lower(e.to_url), 'https://www.', 'https://'), 'http://www.', 'http://'), '/')
    OR (c.channel_id <> '' AND e.to_url ILIKE '%/channel/' || c.channel_id || '%')
    OR (c.identity_key LIKE 'UC%' AND e.to_url ILIKE '%/channel/' || c.identity_key || '%')
    OR (c.identity_key LIKE '@%' AND (
         e.to_url ILIKE '%/' || c.identity_key
         OR e.to_url ILIKE '%/' || c.identity_key || '/%'
    ))
    OR (
      c.platform = 'youtube'
      AND substring(e.to_url from '(?i)youtube\.com/@([^/?#]+)') IS NOT NULL
      AND (
        c.identity_key ILIKE '@' || substring(e.to_url from '(?i)youtube\.com/@([^/?#]+)')
        OR c.uploader ILIKE substring(e.to_url from '(?i)youtube\.com/@([^/?#]+)')
      )
    )
    OR (
      substring(e.to_url from '(?i)(?:twitter|x)\.com/@?([^/?#]+)') IS NOT NULL
      AND c.canonical_url ~* ('https?://(www\.)?(twitter|x)\.com/'
        || substring(e.to_url from '(?i)(?:twitter|x)\.com/@?([^/?#]+)')
        || '/?$')
    )
);

-- ClaimVideosForLinkHarvest marks a batch of attached videos for outlink harvest.
-- name: ClaimVideosForLinkHarvest :many
UPDATE videos
SET links_harvested_at = NOW()
WHERE id IN (
    SELECT v.id
    FROM videos v
    WHERE v.channel_row_id IS NOT NULL
      AND v.links_harvested_at IS NULL
    ORDER BY EXISTS (
        SELECT 1 FROM channels ch
        WHERE ch.id = v.channel_row_id AND ch.creator_id IS NOT NULL
    ) DESC, v.created_at DESC
    LIMIT sqlc.arg(batch_size)::int
    FOR UPDATE OF v SKIP LOCKED
)
RETURNING id, channel_row_id, title, description;

-- MarkVideoLinksHarvested records that title/description/comment outlinks were harvested.
-- name: MarkVideoLinksHarvested :exec
UPDATE videos SET links_harvested_at = NOW() WHERE id = sqlc.arg(id);

-- ListVideosForAnalyze returns the public-metric rows the autopsy analyzer needs.
-- name: ListVideosForAnalyze :many
SELECT v.id, v.src, v.title, v.format, v.upload_date, v.duration_seconds, v.view_count, v.like_count,
  v.comment_count
FROM videos v
WHERE v.channel_row_id = ANY(sqlc.arg(channel_ids)::uuid[])
  AND v.upload_date IS NOT NULL
ORDER BY v.upload_date;
