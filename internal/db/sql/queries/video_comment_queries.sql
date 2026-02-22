-- UpsertVideoCommentsFromJSON ingests a JSON array of yt-dlp comment objects.
-- It extracts common fields for indexing/search and stores the full raw object.
--
-- Required JSON shape: a JSON array of objects. Elements without an id are skipped.
-- name: UpsertVideoCommentsFromJSON :exec
INSERT INTO video_comments (
    video_id,
    source,
    comment_id,
    parent_id,
    author,
    author_id,
    author_url,
    published_at,
    like_count,
    text,
    search,
    raw,
    updated_at
)
SELECT
    sqlc.arg(video_id)::uuid AS video_id,
    sqlc.arg(source)::text AS source,
    c->>'id' AS comment_id,
    COALESCE(NULLIF(c->>'parent', ''), NULLIF(c->>'parent_id', '')) AS parent_id,
    NULLIF(c->>'author', '') AS author,
    NULLIF(c->>'author_id', '') AS author_id,
    NULLIF(c->>'author_url', '') AS author_url,
    CASE
        WHEN (c->>'timestamp') ~ '^[0-9]+(\\.[0-9]+)?$' THEN to_timestamp((c->>'timestamp')::double precision)
        ELSE NULL
    END AS published_at,
    CASE
        WHEN (c->>'like_count') ~ '^-?[0-9]+$' THEN (c->>'like_count')::bigint
        ELSE NULL
    END AS like_count,
    COALESCE(NULLIF(c->>'text', ''), NULLIF(c->>'content', ''), NULLIF(c->>'comment', '')) AS text,
    to_tsvector('simple'::regconfig,
        coalesce(COALESCE(NULLIF(c->>'text', ''), NULLIF(c->>'content', ''), NULLIF(c->>'comment', '')), '')
        || ' ' ||
        coalesce(NULLIF(c->>'author', ''), '')
    ) AS search,
    c AS raw,
    NOW() AS updated_at
FROM jsonb_array_elements(sqlc.arg(comments_json)::jsonb) AS c
WHERE COALESCE(NULLIF(c->>'id', ''), '') <> ''
ON CONFLICT (video_id, source, comment_id)
DO UPDATE SET
    parent_id = EXCLUDED.parent_id,
    author = EXCLUDED.author,
    author_id = EXCLUDED.author_id,
    author_url = EXCLUDED.author_url,
    published_at = EXCLUDED.published_at,
    like_count = EXCLUDED.like_count,
    text = EXCLUDED.text,
    search = EXCLUDED.search,
    raw = EXCLUDED.raw,
    updated_at = NOW();

-- CountVideoComments returns total comments ingested for a video.
-- name: CountVideoComments :one
SELECT COUNT(*)
FROM video_comments
WHERE video_id = $1;

-- ListVideoComments returns paginated top-level comments for a video.
-- Top-level = parent_id IS NULL or parent_id = 'root'.
-- Ordered by like_count DESC, then published_at DESC.
-- name: ListVideoComments :many
SELECT id, video_id, source, comment_id, parent_id, author, author_id, author_url,
       published_at, like_count, text, created_at
FROM video_comments
WHERE video_id = $1
  AND (parent_id IS NULL OR parent_id = 'root')
ORDER BY COALESCE(like_count, 0) DESC, published_at DESC NULLS LAST
LIMIT sqlc.arg(page_size)::int
OFFSET sqlc.arg(page_offset)::int;

-- ListVideoCommentReplies returns replies (children) for a given parent comment.
-- name: ListVideoCommentReplies :many
SELECT id, video_id, source, comment_id, parent_id, author, author_id, author_url,
       published_at, like_count, text, created_at
FROM video_comments
WHERE video_id = $1
  AND parent_id = sqlc.arg(parent_comment_id)::text
ORDER BY published_at ASC NULLS LAST
LIMIT 50;

-- SearchVideoComments returns comments matching a text search query.
-- name: SearchVideoComments :many
SELECT id, video_id, source, comment_id, parent_id, author, author_id, author_url,
       published_at, like_count, text, created_at
FROM video_comments
WHERE video_id = $1
  AND search @@ plainto_tsquery('simple', sqlc.arg(query)::text)
ORDER BY COALESCE(like_count, 0) DESC
LIMIT sqlc.arg(page_size)::int
OFFSET sqlc.arg(page_offset)::int;
