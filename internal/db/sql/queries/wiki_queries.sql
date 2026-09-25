-- name: GetWikiPage :one
SELECT * FROM wiki_pages
WHERE tenant_id = sqlc.arg(tenant_id) AND tree = sqlc.arg(tree) AND slug = sqlc.arg(slug);

-- name: ListWikiPages :many
SELECT * FROM wiki_pages
WHERE tenant_id = sqlc.arg(tenant_id)
  AND (sqlc.arg(tree)::text = '' OR tree = sqlc.arg(tree))
ORDER BY tree, slug;

-- name: ListWikiPagesForCreator :many
SELECT * FROM wiki_pages
WHERE tenant_id = sqlc.arg(tenant_id) AND creator_id = sqlc.arg(creator_id)
ORDER BY tree, slug;

-- name: ListWikiPagesForChannel :many
SELECT * FROM wiki_pages
WHERE tenant_id = sqlc.arg(tenant_id) AND channel_id = sqlc.arg(channel_id)
ORDER BY tree, slug;

-- name: SearchWikiPages :many
SELECT p.*,
       ts_rank(s.search, websearch_to_tsquery('simple', sqlc.arg(query)))::float8 AS rank
FROM wiki_pages p
JOIN wiki_search s ON s.tenant_id = p.tenant_id AND s.tree = p.tree AND s.slug = p.slug
WHERE s.search @@ websearch_to_tsquery('simple', sqlc.arg(query))
  AND p.tenant_id = sqlc.arg(tenant_id)
  AND (sqlc.arg(tree)::text = '' OR p.tree = sqlc.arg(tree))
ORDER BY rank DESC, p.updated_at DESC
LIMIT sqlc.arg(page_limit);

-- name: InsertWikiPage :one
INSERT INTO wiki_pages (
    tenant_id, tree, slug, title, body, revision, creator_id, channel_id, updated_by
) VALUES (
    sqlc.arg(tenant_id), sqlc.arg(tree),
    sqlc.arg(slug),
    sqlc.arg(title),
    sqlc.arg(body),
    1,
    sqlc.narg(creator_id),
    sqlc.narg(channel_id),
    sqlc.arg(updated_by)
)
RETURNING *;

-- name: UpdateWikiPage :one
UPDATE wiki_pages
SET title = sqlc.arg(title),
    body = sqlc.arg(body),
    revision = revision + 1,
    creator_id = sqlc.narg(creator_id),
    channel_id = sqlc.narg(channel_id),
    updated_by = sqlc.arg(updated_by),
    updated_at = NOW()
WHERE tree = sqlc.arg(tree)
  AND tenant_id = sqlc.arg(tenant_id)
  AND slug = sqlc.arg(slug)
  AND revision = sqlc.arg(expected_revision)
RETURNING *;

-- name: InsertWikiRevision :one
INSERT INTO wiki_revisions (
    tenant_id, tree, slug, revision, title, body, diff, summary,
    actor_kind, actor_id, user_id, session_id,
    client_name, client_version, token_name
) VALUES (
    sqlc.arg(tenant_id), sqlc.arg(tree),
    sqlc.arg(slug),
    sqlc.arg(revision),
    sqlc.arg(title),
    sqlc.arg(body),
    sqlc.arg(diff),
    sqlc.arg(summary),
    sqlc.arg(actor_kind),
    sqlc.arg(actor_id),
    sqlc.narg(user_id),
    sqlc.arg(session_id),
    sqlc.arg(client_name),
    sqlc.arg(client_version),
    sqlc.arg(token_name)
)
RETURNING *;

-- name: ListWikiRevisions :many
SELECT * FROM wiki_revisions
WHERE tree = sqlc.arg(tree) AND slug = sqlc.arg(slug)
  AND tenant_id = sqlc.arg(tenant_id)
ORDER BY revision DESC;

-- name: GetWikiRevision :one
SELECT * FROM wiki_revisions
WHERE tenant_id = sqlc.arg(tenant_id) AND tree = sqlc.arg(tree) AND slug = sqlc.arg(slug) AND revision = sqlc.arg(revision);

-- name: DeleteWikiLinksForPage :exec
DELETE FROM wiki_links
WHERE tenant_id = sqlc.arg(tenant_id)
  AND from_tree = sqlc.arg(tree) AND from_slug = sqlc.arg(slug);

-- name: InsertWikiLink :exec
INSERT INTO wiki_links (tenant_id, from_tree, from_slug, to_tree, to_slug)
VALUES (sqlc.arg(tenant_id), sqlc.arg(from_tree), sqlc.arg(from_slug), sqlc.arg(to_tree), sqlc.arg(to_slug))
ON CONFLICT DO NOTHING;

-- name: ListWikiBacklinks :many
SELECT p.tree, p.slug, p.title, p.revision, p.updated_at
FROM wiki_links l
JOIN wiki_pages p ON p.tenant_id = l.tenant_id AND p.tree = l.from_tree AND p.slug = l.from_slug
WHERE l.tenant_id = sqlc.arg(tenant_id) AND l.to_tree = sqlc.arg(tree) AND l.to_slug = sqlc.arg(slug)
ORDER BY p.tree, p.slug;

-- name: ListWikiLinksFromPage :many
SELECT to_tree, to_slug
FROM wiki_links
WHERE tenant_id = sqlc.arg(tenant_id) AND from_tree = sqlc.arg(tree) AND from_slug = sqlc.arg(slug)
ORDER BY to_tree, to_slug;

-- name: ListWikiLinks :many
SELECT from_tree, from_slug, to_tree, to_slug
FROM wiki_links
WHERE tenant_id = sqlc.arg(tenant_id)
ORDER BY from_tree, from_slug, to_tree, to_slug;
