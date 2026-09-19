-- CreateCreator inserts a named person that one or more channels can belong to.
-- name: CreateCreator :one
INSERT INTO creators (name, notes, search)
VALUES (sqlc.arg(name), COALESCE(sqlc.narg(notes), ''), setweight(to_tsvector('simple', sqlc.arg(name)), 'A'))
RETURNING *;

-- ListCreators returns every creator with how many channels are linked.
-- name: ListCreators :many
SELECT c.*, COUNT(ch.id)::bigint AS channel_count
FROM creators c LEFT JOIN channels ch ON ch.creator_id = c.id
GROUP BY c.id ORDER BY c.name;

-- GetCreator fetches one creator by id.
-- name: GetCreator :one
SELECT * FROM creators WHERE id = sqlc.arg(id);

-- SetChannelCreator assigns (or clears, when creator_id is NULL) a channel's creator.
-- name: SetChannelCreator :exec
UPDATE channels SET creator_id = sqlc.narg(creator_id), updated_at = NOW() WHERE id = sqlc.arg(id);

-- ListChannelsByCreator returns the channels that belong to a creator.
-- name: ListChannelsByCreator :many
SELECT * FROM channels WHERE creator_id = sqlc.arg(creator_id) ORDER BY platform, uploader;

-- ListUnassignedChannels returns channels that are not yet linked to a creator.
-- name: ListUnassignedChannels :many
SELECT * FROM channels WHERE creator_id IS NULL ORDER BY uploader LIMIT 200;

-- SearchUnassignedChannels ranks unlinked channels by name/url match.
-- Empty query returns no rows — callers pass a creator name for suggestions.
-- name: SearchUnassignedChannels :many
SELECT
    c.id,
    c.platform,
    c.identity_key,
    c.uploader,
    c.canonical_url,
    (SELECT COUNT(*) FROM videos v WHERE v.channel_row_id = c.id)::bigint AS video_count,
    similarity(c.uploader, sqlc.arg(query))::float4 AS score
FROM channels c
WHERE c.creator_id IS NULL
  AND btrim(sqlc.arg(query)::text) <> ''
  AND (
    c.uploader ILIKE '%' || sqlc.arg(query) || '%'
    OR c.identity_key ILIKE '%' || sqlc.arg(query) || '%'
    OR c.canonical_url ILIKE '%' || sqlc.arg(query) || '%'
    OR c.platform ILIKE '%' || sqlc.arg(query) || '%'
    OR similarity(c.uploader, sqlc.arg(query)) > 0.35
  )
ORDER BY
    similarity(c.uploader, sqlc.arg(query)) DESC,
    video_count DESC,
    c.uploader
LIMIT sqlc.arg(page_limit);

-- LinkChannelsToCreator assigns many unassigned channels to one creator.
-- name: LinkChannelsToCreator :exec
UPDATE channels
SET creator_id = sqlc.arg(creator_id),
    updated_at = NOW()
WHERE id = ANY(sqlc.arg(ids)::uuid[])
  AND creator_id IS NULL;

-- UnlinkChannelFromCreator clears a channel's creator only if it currently belongs to them.
-- name: UnlinkChannelFromCreator :exec
UPDATE channels
SET creator_id = NULL,
    updated_at = NOW()
WHERE id = sqlc.arg(id)
  AND creator_id = sqlc.arg(creator_id);

-- GetCreatorByNameCI finds a creator by case-insensitive name.
-- name: GetCreatorByNameCI :one
SELECT * FROM creators WHERE lower(name) = lower(sqlc.arg(name)) LIMIT 1;

-- CreateCreatorBundle inserts a named grouping of creators (e.g. Gas Digital).
-- name: CreateCreatorBundle :one
INSERT INTO creator_bundles (name, notes, search)
VALUES (sqlc.arg(name), COALESCE(sqlc.narg(notes), ''), setweight(to_tsvector('simple', sqlc.arg(name)), 'A'))
RETURNING *;

-- ListCreatorBundles returns every bundle with how many creators it contains.
-- name: ListCreatorBundles :many
SELECT b.*, COUNT(m.creator_id)::bigint AS member_count
FROM creator_bundles b
LEFT JOIN creator_bundle_members m ON m.bundle_id = b.id
GROUP BY b.id
ORDER BY b.name;

-- GetCreatorBundle fetches one bundle by id.
-- name: GetCreatorBundle :one
SELECT * FROM creator_bundles WHERE id = sqlc.arg(id);

-- ListCreatorBundleMembers returns the creators in a bundle.
-- name: ListCreatorBundleMembers :many
SELECT c.*
FROM creator_bundle_members m
JOIN creators c ON c.id = m.creator_id
WHERE m.bundle_id = sqlc.arg(bundle_id)
ORDER BY c.name;

-- ListBundlesForCreator returns bundles that include this creator.
-- name: ListBundlesForCreator :many
SELECT b.*
FROM creator_bundle_members m
JOIN creator_bundles b ON b.id = m.bundle_id
WHERE m.creator_id = sqlc.arg(creator_id)
ORDER BY b.name;

-- AddCreatorBundleMember links a creator into a bundle.
-- name: AddCreatorBundleMember :exec
INSERT INTO creator_bundle_members (bundle_id, creator_id)
VALUES (sqlc.arg(bundle_id), sqlc.arg(creator_id))
ON CONFLICT (bundle_id, creator_id) DO NOTHING;

-- RemoveCreatorBundleMember unlinks a creator from a bundle.
-- name: RemoveCreatorBundleMember :exec
DELETE FROM creator_bundle_members
WHERE bundle_id = sqlc.arg(bundle_id)
  AND creator_id = sqlc.arg(creator_id);

-- ListResolvedOutlinks returns archived-to-archived outlink edges for creator grouping.
-- name: ListResolvedOutlinks :many
SELECT
    e.from_channel_id,
    e.to_channel_id,
    e.evidence,
    e.weight,
    fc.uploader AS from_uploader,
    fc.creator_id AS from_creator_id,
    tc.uploader AS to_uploader,
    tc.creator_id AS to_creator_id
FROM channel_edges e
JOIN channels fc ON fc.id = e.from_channel_id
JOIN channels tc ON tc.id = e.to_channel_id
WHERE e.kind = 'outlink'
  AND e.to_channel_id IS NOT NULL;

-- ListLabeledUnresolvedOutlinks returns outlinks that still point at a URL, with evidence.
-- name: ListLabeledUnresolvedOutlinks :many
SELECT
    e.from_channel_id,
    e.to_url,
    e.evidence,
    e.weight,
    fc.uploader AS from_uploader,
    fc.creator_id AS from_creator_id,
    fc.platform AS from_platform
FROM channel_edges e
JOIN channels fc ON fc.id = e.from_channel_id
WHERE e.kind = 'outlink'
  AND e.to_channel_id IS NULL
  AND e.to_url <> '';

-- ListMutualOutlinkPairs returns unique A↔B outlink pairs between archived channels.
-- name: ListMutualOutlinkPairs :many
SELECT
    a.from_channel_id AS a_id,
    a.to_channel_id AS b_id,
    fa.uploader AS a_uploader,
    fa.creator_id AS a_creator_id,
    fb.uploader AS b_uploader,
    fb.creator_id AS b_creator_id
FROM channel_edges a
JOIN channel_edges b
  ON a.from_channel_id = b.to_channel_id
 AND a.to_channel_id = b.from_channel_id
 AND a.from_channel_id < a.to_channel_id
JOIN channels fa ON fa.id = a.from_channel_id
JOIN channels fb ON fb.id = a.to_channel_id
WHERE a.kind = 'outlink'
  AND b.kind = 'outlink'
  AND a.to_channel_id IS NOT NULL;

-- GetCreatorSuggestionByKey looks up a nomination by its stable channel-set key.
-- name: GetCreatorSuggestionByKey :one
SELECT * FROM creator_suggestions WHERE channel_key = sqlc.arg(channel_key);

-- InsertCreatorSuggestion records a pending new-creator or add-to-creator nomination.
-- name: InsertCreatorSuggestion :one
INSERT INTO creator_suggestions (kind, creator_id, proposed_name, reason, evidence, channel_key)
VALUES (
    sqlc.arg(kind),
    sqlc.narg(creator_id),
    sqlc.arg(proposed_name),
    sqlc.arg(reason),
    sqlc.arg(evidence),
    sqlc.arg(channel_key)
)
RETURNING *;

-- AddCreatorSuggestionMember attaches a channel to a suggestion.
-- name: AddCreatorSuggestionMember :exec
INSERT INTO creator_suggestion_members (suggestion_id, channel_id)
VALUES (sqlc.arg(suggestion_id), sqlc.arg(channel_id))
ON CONFLICT DO NOTHING;

-- ListPendingCreatorSuggestions returns open nominations.
-- name: ListPendingCreatorSuggestions :many
SELECT s.*,
  COALESCE((
    SELECT string_agg(c.uploader, ', ' ORDER BY c.uploader)
    FROM creator_suggestion_members m
    JOIN channels c ON c.id = m.channel_id
    WHERE m.suggestion_id = s.id
  ), '')::text AS channel_names
FROM creator_suggestions s
WHERE s.status = 'pending'
ORDER BY s.created_at DESC;

-- ListCreatorSuggestionMembers returns the channels on one suggestion.
-- name: ListCreatorSuggestionMembers :many
SELECT c.*
FROM creator_suggestion_members m
JOIN channels c ON c.id = m.channel_id
WHERE m.suggestion_id = sqlc.arg(suggestion_id)
ORDER BY c.uploader;

-- GetCreatorSuggestion fetches one nomination.
-- name: GetCreatorSuggestion :one
SELECT * FROM creator_suggestions WHERE id = sqlc.arg(id);

-- SetCreatorSuggestionStatus accepts or dismisses a nomination.
-- name: SetCreatorSuggestionStatus :exec
UPDATE creator_suggestions
SET status = sqlc.arg(status),
    updated_at = NOW()
WHERE id = sqlc.arg(id)
  AND status = 'pending';
