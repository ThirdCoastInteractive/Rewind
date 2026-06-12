-- UpsertTag inserts a tag (keyed by slug) or returns the existing one. The name
-- is refreshed so the latest casing wins; color is left as-is on conflict.
-- name: UpsertTag :one
INSERT INTO tags (name, slug, color, created_by)
VALUES (sqlc.arg(name), sqlc.arg(slug), sqlc.narg(color), sqlc.narg(created_by))
ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name
RETURNING *;

-- AddVideoTag links a tag to a video (idempotent).
-- name: AddVideoTag :exec
INSERT INTO video_tags (video_id, tag_id, created_by)
VALUES (sqlc.arg(video_id), sqlc.arg(tag_id), sqlc.narg(created_by))
ON CONFLICT (video_id, tag_id) DO NOTHING;

-- AddVideoTagToMany links one tag to many videos at once (idempotent). Drives
-- the library bulk-tag action.
-- name: AddVideoTagToMany :exec
INSERT INTO video_tags (video_id, tag_id, created_by)
SELECT v, sqlc.arg(tag_id), sqlc.narg(created_by)
FROM unnest(sqlc.arg(video_ids)::uuid[]) AS v
ON CONFLICT (video_id, tag_id) DO NOTHING;

-- RemoveVideoTag unlinks a tag from a video.
-- name: RemoveVideoTag :exec
DELETE FROM video_tags
WHERE video_id = sqlc.arg(video_id) AND tag_id = sqlc.arg(tag_id);

-- ListTagsForVideo returns a video's tags, alphabetically.
-- name: ListTagsForVideo :many
SELECT t.id, t.name, t.slug, t.color
FROM tags t
JOIN video_tags vt ON vt.tag_id = t.id
WHERE vt.video_id = sqlc.arg(video_id)
ORDER BY t.name ASC;

-- ListAllTagsWithCounts returns every tag with how many videos carry it,
-- most-used first. Drives the library tag filter/sidebar.
-- name: ListAllTagsWithCounts :many
SELECT t.id, t.name, t.slug, t.color, COUNT(vt.video_id)::bigint AS video_count
FROM tags t
LEFT JOIN video_tags vt ON vt.tag_id = t.id
GROUP BY t.id
ORDER BY video_count DESC, t.name ASC;
