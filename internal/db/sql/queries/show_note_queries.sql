-- ============================================================================
-- Show notes (producer v2 central entity: content + hosts + live session)
-- ============================================================================

-- name: ListShowNotesForUser :many
-- Show notes the user owns or is a host/viewer on, newest-updated first.
SELECT DISTINCT sn.id, sn.owner_id, sn.title, sn.description, sn.is_live,
       sn.live_started_at, sn.public_code, sn.created_at, sn.updated_at, sn.tenant_id
FROM show_notes sn
LEFT JOIN show_note_hosts h ON h.show_note_id = sn.id
WHERE sn.owner_id = sqlc.arg(user_id) OR h.user_id = sqlc.arg(user_id)
ORDER BY sn.updated_at DESC;

-- name: GetShowNote :one
SELECT * FROM show_notes WHERE id = sqlc.arg(id);

-- name: GetShowNoteByPublicCode :one
SELECT * FROM show_notes WHERE public_code = sqlc.arg(public_code);

-- name: CreateShowNote :one
INSERT INTO show_notes (owner_id, title, tenant_id)
VALUES (sqlc.arg(owner_id), sqlc.arg(title), sqlc.arg(tenant_id))
RETURNING *;

-- name: UpdateShowNote :one
UPDATE show_notes
SET title           = COALESCE(sqlc.narg(title), title),
    description     = COALESCE(sqlc.narg(description), description),
    is_live         = COALESCE(sqlc.narg(is_live), is_live),
    live_started_at = COALESCE(sqlc.narg(live_started_at), live_started_at),
    public_code     = COALESCE(sqlc.narg(public_code), public_code),
    updated_at      = NOW()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: TouchShowNote :exec
UPDATE show_notes SET updated_at = NOW() WHERE id = sqlc.arg(id);

-- name: SetShowNoteScene :exec
-- Persist the active composited-scene JSON for a live show note.
UPDATE show_notes SET scene_state = sqlc.arg(scene_state), updated_at = NOW()
WHERE id = sqlc.arg(id);

-- name: DeleteShowNote :exec
DELETE FROM show_notes
WHERE id = sqlc.arg(id) AND owner_id = sqlc.arg(owner_id);

-- ============================================================================
-- Show note blocks (nested content tree)
-- ============================================================================

-- name: ListBlocksForShowNote :many
-- Flat load for in-memory tree assembly (see shownote.BuildTree).
SELECT * FROM show_note_blocks
WHERE show_note_id = sqlc.arg(show_note_id)
ORDER BY parent_id NULLS FIRST, position ASC, created_at ASC;

-- name: GetBlock :one
SELECT * FROM show_note_blocks WHERE id = sqlc.arg(id);

-- name: NoteReferencesVideo :one
-- Authorizes viewer-scoped content streaming: true if the video is referenced by
-- a block in this show note.
SELECT COUNT(*) > 0
FROM show_note_blocks b
JOIN show_notes sn ON sn.id = b.show_note_id
JOIN videos v ON v.id = b.video_id AND v.tenant_id = sn.tenant_id
WHERE b.show_note_id = sqlc.arg(show_note_id) AND b.video_id = sqlc.arg(video_id);

-- name: CreateBlock :one
INSERT INTO show_note_blocks (
    show_note_id, parent_id, block_type, title, notes, video_id, clip_id, position, duration_override
) VALUES (
    sqlc.arg(show_note_id), sqlc.narg(parent_id), sqlc.arg(block_type), sqlc.arg(title), sqlc.arg(notes),
    sqlc.narg(video_id), sqlc.narg(clip_id), sqlc.arg(position), sqlc.narg(duration_override)
) RETURNING *;

-- name: UpdateBlockContent :one
-- Inline edits to a block's title / cue notes / duration override.
UPDATE show_note_blocks
SET title             = COALESCE(sqlc.narg(title), title),
    notes             = COALESCE(sqlc.narg(notes), notes),
    duration_override = COALESCE(sqlc.narg(duration_override), duration_override)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: NextBlockPosition :one
-- Append position for a new block under a parent (NULL-safe sibling match).
SELECT COALESCE(MAX(position) + 1, 0)::int AS next_position
FROM show_note_blocks
WHERE show_note_id = sqlc.arg(show_note_id)
  AND parent_id IS NOT DISTINCT FROM sqlc.narg(parent_id);

-- name: SetBlockParentAndPosition :exec
-- Reparent + reorder a block (drag-drop). Caller renormalizes sibling positions.
UPDATE show_note_blocks
SET parent_id = sqlc.narg(parent_id), position = sqlc.arg(position)
WHERE id = sqlc.arg(id);

-- name: SetBlockPosition :exec
UPDATE show_note_blocks
SET position = sqlc.arg(position)
WHERE id = sqlc.arg(id);

-- name: DeleteBlock :exec
-- Children cascade via the self-FK.
DELETE FROM show_note_blocks WHERE id = sqlc.arg(id);

-- ============================================================================
-- Show note hosts (access roster)
-- ============================================================================

-- name: ListHostsForShowNote :many
SELECT h.id, h.user_id, h.role, h.created_at, u.user_name AS username
FROM show_note_hosts h
JOIN users u ON u.id = h.user_id
WHERE h.show_note_id = sqlc.arg(show_note_id)
ORDER BY h.created_at ASC;

-- name: AddHost :one
INSERT INTO show_note_hosts (show_note_id, user_id, role)
VALUES (sqlc.arg(show_note_id), sqlc.arg(user_id), sqlc.arg(role))
ON CONFLICT (show_note_id, user_id) DO UPDATE SET role = EXCLUDED.role
RETURNING *;

-- name: GetHostRole :one
-- Authorization check used by mutating handlers.
SELECT role FROM show_note_hosts
WHERE show_note_id = sqlc.arg(show_note_id) AND user_id = sqlc.arg(user_id);

-- name: RemoveHost :exec
DELETE FROM show_note_hosts
WHERE show_note_id = sqlc.arg(show_note_id) AND user_id = sqlc.arg(user_id);
