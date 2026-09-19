-- ============================================================================
-- Collaborative Markdown show workspace
-- ============================================================================

-- name: ListenShowNoteRoomEvents :exec
LISTEN show_note_room_events;

-- name: GetShowNoteDocument :one
SELECT * FROM show_note_documents WHERE show_note_id = sqlc.arg(show_note_id);

-- name: LockShowNoteDocument :one
SELECT * FROM show_note_documents
WHERE show_note_id = sqlc.arg(show_note_id)
FOR UPDATE;

-- name: CreateShowNoteDocument :one
INSERT INTO show_note_documents (show_note_id, markdown, revision, snapshot, snapshot_revision)
VALUES (sqlc.arg(show_note_id), sqlc.arg(markdown), sqlc.arg(revision), sqlc.arg(snapshot), sqlc.arg(snapshot_revision))
ON CONFLICT (show_note_id) DO NOTHING
RETURNING *;

-- name: UpdateShowNoteDocumentProjection :one
UPDATE show_note_documents
SET markdown = sqlc.arg(markdown),
    updated_at = NOW()
WHERE show_note_id = sqlc.arg(show_note_id)
RETURNING *;

-- name: ShowNoteDocumentExists :one
SELECT EXISTS (
    SELECT 1 FROM show_note_documents WHERE show_note_id = sqlc.arg(show_note_id)
);

-- name: LockShowNoteForWorkspaceMigration :one
SELECT id FROM show_notes WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: ListAllShowNoteIDs :many
SELECT id FROM show_notes ORDER BY created_at, id;

-- name: ListShowNotesPendingWorkspaceMigration :many
SELECT * FROM show_notes
WHERE workspace_migrated_at IS NULL
ORDER BY created_at, id;

-- name: MarkShowNoteWorkspaceMigrated :exec
UPDATE show_notes
SET workspace_migrated_at = NOW(), workspace_migration_error = '', updated_at = NOW()
WHERE id = sqlc.arg(id);

-- name: MarkShowNoteWorkspaceMigrationFailed :exec
UPDATE show_notes
SET workspace_migration_error = sqlc.arg(workspace_migration_error)
WHERE id = sqlc.arg(id);

-- name: ListShowNoteDocumentUpdates :many
SELECT * FROM show_note_document_updates
WHERE show_note_id = sqlc.arg(show_note_id)
ORDER BY revision;

-- name: ListShowNoteDocumentUpdatesAfter :many
SELECT * FROM show_note_document_updates
WHERE show_note_id = sqlc.arg(show_note_id) AND revision > sqlc.arg(revision)
ORDER BY revision;

-- name: AppendShowNoteDocumentUpdate :exec
INSERT INTO show_note_document_updates (show_note_id, revision, update)
VALUES (sqlc.arg(show_note_id), sqlc.arg(revision), sqlc.arg(update));

-- name: CommitShowNoteDocumentUpdateProjection :exec
UPDATE show_note_documents
SET markdown = sqlc.arg(markdown), revision = sqlc.arg(revision), updated_at = NOW()
WHERE show_note_id = sqlc.arg(show_note_id);

-- name: StoreShowNoteDocumentSnapshot :exec
UPDATE show_note_documents
SET snapshot = sqlc.arg(snapshot), snapshot_revision = sqlc.arg(snapshot_revision)
WHERE show_note_id = sqlc.arg(show_note_id);

-- name: DeleteShowNoteDocumentUpdatesThrough :exec
DELETE FROM show_note_document_updates
WHERE show_note_id = sqlc.arg(show_note_id) AND revision <= sqlc.arg(revision);

-- name: ListShowNoteReferences :many
SELECT * FROM show_note_references
WHERE show_note_id = sqlc.arg(show_note_id)
ORDER BY ordinal;

-- name: GetShowNoteReference :one
SELECT * FROM show_note_references
WHERE id = sqlc.arg(id) AND show_note_id = sqlc.arg(show_note_id);

-- name: GetShowNoteReferenceByOccurrence :one
SELECT * FROM show_note_references
WHERE show_note_id = sqlc.arg(show_note_id)
  AND occurrence_key = sqlc.arg(occurrence_key);

-- name: ShowNoteVideoObjectExists :one
SELECT EXISTS (
    SELECT 1
    FROM videos
    WHERE id = sqlc.arg(id)
      AND media <> 'metadata'
      AND video_path IS NOT NULL
      AND btrim(video_path) <> ''
);

-- name: ShowNoteClipObjectExists :one
SELECT EXISTS (SELECT 1 FROM clips WHERE id = sqlc.arg(id));

-- name: ShowNoteMarkerObjectExists :one
SELECT EXISTS (SELECT 1 FROM markers WHERE id = sqlc.arg(id));

-- name: ListPlayableShowNoteReferences :many
SELECT r.occurrence_key, r.ordinal, r.kind, r.label, r.status,
       COALESCE(r.video_id, c.video_id, m.video_id) AS video_id,
       CASE
           WHEN r.clip_id IS NOT NULL THEN c.start_ts
           WHEN r.marker_id IS NOT NULL THEN m.timestamp
           ELSE COALESCE(r.start_seconds, 0)
       END::double precision AS start_seconds,
       COALESCE(CASE WHEN r.clip_id IS NOT NULL THEN c.end_ts ELSE r.end_seconds END, 0)::double precision AS end_seconds
FROM show_note_references r
LEFT JOIN clips c ON c.id = r.clip_id
LEFT JOIN markers m ON m.id = r.marker_id
JOIN videos v ON v.id = COALESCE(r.video_id, c.video_id, m.video_id)
WHERE r.show_note_id = sqlc.arg(show_note_id)
  AND r.status = 'ready'
  AND COALESCE(r.video_id, c.video_id, m.video_id) IS NOT NULL
  AND v.media <> 'metadata'
  AND v.video_path IS NOT NULL
  AND btrim(v.video_path) <> ''
ORDER BY r.ordinal;

-- name: DeleteShowNoteReferenceProjection :exec
DELETE FROM show_note_references
WHERE show_note_id = sqlc.arg(show_note_id);

-- name: DeleteStaleShowNoteReferences :exec
DELETE FROM show_note_references
WHERE show_note_id = sqlc.arg(show_note_id) AND parsed_revision <> sqlc.arg(parsed_revision);

-- name: CountShowNoteReferencesAtRevision :one
SELECT COUNT(*) FROM show_note_references
WHERE show_note_id = sqlc.arg(show_note_id) AND parsed_revision = sqlc.arg(parsed_revision);

-- name: ListWorkspacePreflightDocuments :many
SELECT sn.id, d.markdown, d.revision
FROM show_notes sn
LEFT JOIN show_note_documents d ON d.show_note_id = sn.id
ORDER BY sn.created_at, sn.id;

-- name: CountPendingWorkspaceMigrations :one
SELECT COUNT(*) FROM show_notes sn
LEFT JOIN show_note_documents d ON d.show_note_id = sn.id
WHERE sn.workspace_migrated_at IS NULL OR d.show_note_id IS NULL;

-- name: UpsertShowNoteReference :one
INSERT INTO show_note_references (
    show_note_id, occurrence_key, ordinal, kind, source_uri, label, context,
    section_path, start_seconds, end_seconds, status, video_id, clip_id,
    marker_id, line_start, line_end, parsed_revision, diagnostic
) VALUES (
    sqlc.arg(show_note_id), sqlc.arg(occurrence_key), sqlc.arg(ordinal), sqlc.arg(kind),
    sqlc.arg(source_uri), sqlc.arg(label), sqlc.arg(context), sqlc.arg(section_path),
    sqlc.narg(start_seconds), sqlc.narg(end_seconds), sqlc.arg(status), sqlc.narg(video_id),
    sqlc.narg(clip_id), sqlc.narg(marker_id), sqlc.arg(line_start), sqlc.arg(line_end),
    sqlc.arg(parsed_revision), sqlc.arg(diagnostic)
)
ON CONFLICT (show_note_id, occurrence_key) DO UPDATE SET
    ordinal = EXCLUDED.ordinal,
    kind = EXCLUDED.kind,
    source_uri = EXCLUDED.source_uri,
    label = EXCLUDED.label,
    context = EXCLUDED.context,
    section_path = EXCLUDED.section_path,
    start_seconds = EXCLUDED.start_seconds,
    end_seconds = EXCLUDED.end_seconds,
    status = CASE
        WHEN show_note_references.source_uri = EXCLUDED.source_uri
         AND show_note_references.status = 'resolving'
         AND EXCLUDED.status = 'unresolved'
        THEN show_note_references.status ELSE EXCLUDED.status END,
    video_id = CASE
        WHEN show_note_references.source_uri = EXCLUDED.source_uri
         AND show_note_references.status = 'resolving'
         AND EXCLUDED.status = 'unresolved'
        THEN show_note_references.video_id ELSE EXCLUDED.video_id END,
    clip_id = CASE
        WHEN show_note_references.source_uri = EXCLUDED.source_uri
         AND show_note_references.status = 'resolving'
         AND EXCLUDED.status = 'unresolved'
        THEN show_note_references.clip_id ELSE EXCLUDED.clip_id END,
    marker_id = CASE
        WHEN show_note_references.source_uri = EXCLUDED.source_uri
         AND show_note_references.status = 'resolving'
         AND EXCLUDED.status = 'unresolved'
        THEN show_note_references.marker_id ELSE EXCLUDED.marker_id END,
    line_start = EXCLUDED.line_start,
    line_end = EXCLUDED.line_end,
    parsed_revision = EXCLUDED.parsed_revision,
    diagnostic = EXCLUDED.diagnostic,
    updated_at = NOW()
RETURNING *;

-- name: FindVideoForShowNoteSource :one
SELECT * FROM videos
WHERE src = sqlc.arg(source_uri)
  AND media <> 'metadata'
  AND video_path IS NOT NULL
  AND btrim(video_path) <> ''
ORDER BY created_at DESC
LIMIT 1;

-- name: MarkShowNoteReferenceResolving :one
UPDATE show_note_references
SET status = 'resolving', download_job_id = sqlc.arg(download_job_id), diagnostic = '', updated_at = NOW()
WHERE id = sqlc.arg(id) AND show_note_id = sqlc.arg(show_note_id)
RETURNING *;

-- name: GetShowNoteReferenceDownloadJob :one
SELECT dj.* FROM show_note_references r
JOIN download_jobs dj ON dj.id = r.download_job_id
WHERE r.id = sqlc.arg(id) AND r.show_note_id = sqlc.arg(show_note_id);

-- name: CreateShowNoteMarker :one
INSERT INTO markers (
    video_id, timestamp, title, description, color, marker_type, duration,
    created_by, source, source_ref
) VALUES (
    sqlc.arg(video_id), sqlc.arg(timestamp), sqlc.arg(title), sqlc.arg(description),
    '#a67c52', 'point', NULL, sqlc.arg(created_by), 'show-note', sqlc.arg(source_ref)
)
ON CONFLICT (video_id, source, (round(timestamp::numeric, 0)), source_ref)
WHERE source <> 'user'
DO UPDATE SET source_ref = EXCLUDED.source_ref
RETURNING *;

-- name: CreateShowNoteClip :one
INSERT INTO clips (
    video_id, start_ts, end_ts, duration, created_by, title, description, source, source_ref
) VALUES (
    sqlc.arg(video_id), sqlc.arg(start_ts), sqlc.arg(end_ts),
    sqlc.arg(end_ts) - sqlc.arg(start_ts), sqlc.arg(created_by),
    sqlc.arg(title), sqlc.arg(description), 'show-note', sqlc.arg(source_ref)
)
ON CONFLICT (source, source_ref)
WHERE source <> 'user' AND source_ref <> ''
DO UPDATE SET source_ref = EXCLUDED.source_ref
RETURNING *;

-- name: NoteWorkspaceReferencesVideo :one
SELECT EXISTS (
    SELECT 1 FROM show_note_references r
    LEFT JOIN clips c ON c.id = r.clip_id
    LEFT JOIN markers m ON m.id = r.marker_id
    WHERE r.show_note_id = sqlc.arg(show_note_id)
      AND (r.video_id = sqlc.arg(video_id) OR c.video_id = sqlc.arg(video_id) OR m.video_id = sqlc.arg(video_id))
);

-- name: CreateShowNoteRoomEvent :one
INSERT INTO show_note_room_events (
    show_note_id, event_type, actor_kind, actor_user_id, actor_token_id, actor_name, payload
) VALUES (
    sqlc.arg(show_note_id), sqlc.arg(event_type), sqlc.arg(actor_kind), sqlc.narg(actor_user_id),
    sqlc.narg(actor_token_id), sqlc.arg(actor_name), sqlc.arg(payload)
) RETURNING *;

-- name: ListShowNoteRoomEventsAfter :many
SELECT * FROM show_note_room_events
WHERE show_note_id = sqlc.arg(show_note_id) AND cursor > sqlc.arg(cursor)
ORDER BY cursor
LIMIT sqlc.arg(result_limit);

-- name: GetShowNoteRoomCursor :one
SELECT COALESCE(MAX(cursor), 0)::bigint FROM show_note_room_events
WHERE show_note_id = sqlc.arg(show_note_id);

-- name: CreateShowNoteRoomMessage :one
INSERT INTO show_note_room_messages (
    show_note_id, event_cursor, actor_kind, actor_user_id, actor_token_id,
    actor_name, body, reply_to
) VALUES (
    sqlc.arg(show_note_id), sqlc.narg(event_cursor), sqlc.arg(actor_kind), sqlc.narg(actor_user_id),
    sqlc.narg(actor_token_id), sqlc.arg(actor_name), sqlc.arg(body), sqlc.narg(reply_to)
) RETURNING *;

-- name: ListShowNoteRoomMessages :many
SELECT * FROM show_note_room_messages
WHERE show_note_id = sqlc.arg(show_note_id)
ORDER BY created_at DESC
LIMIT sqlc.arg(result_limit);

-- name: SetShowNoteRoomMessageEventCursor :exec
UPDATE show_note_room_messages
SET event_cursor = sqlc.arg(event_cursor)
WHERE id = sqlc.arg(id);

-- name: CreateShowNoteReviewThread :one
INSERT INTO show_note_review_threads (
    show_note_id, kind, actor_kind, actor_user_id, actor_token_id, actor_name,
    body, summary, base_revision, base_markdown, expected_text, patch, anchor_start, anchor_end,
    start_line, start_column, end_line, end_column
) VALUES (
    sqlc.arg(show_note_id), sqlc.arg(kind), sqlc.arg(actor_kind), sqlc.narg(actor_user_id),
    sqlc.narg(actor_token_id), sqlc.arg(actor_name), sqlc.arg(body), sqlc.arg(summary),
    sqlc.arg(base_revision), sqlc.arg(base_markdown), sqlc.arg(expected_text), sqlc.arg(patch), sqlc.arg(anchor_start),
    sqlc.arg(anchor_end), sqlc.arg(start_line), sqlc.arg(start_column), sqlc.arg(end_line),
    sqlc.arg(end_column)
) RETURNING *;

-- name: GetShowNoteReviewThread :one
SELECT * FROM show_note_review_threads WHERE id = sqlc.arg(id);

-- name: ListShowNoteReviewThreads :many
SELECT * FROM show_note_review_threads
WHERE show_note_id = sqlc.arg(show_note_id)
ORDER BY created_at;

-- name: UpdateShowNoteReviewStatus :one
UPDATE show_note_review_threads
SET status = sqlc.arg(status),
    detached = sqlc.arg(detached),
    closed_at = CASE WHEN sqlc.arg(status)::text = 'open' THEN NULL ELSE NOW() END,
    closed_by_user_id = CASE WHEN sqlc.arg(status)::text = 'open' THEN NULL ELSE sqlc.narg(closed_by_user_id) END,
    updated_at = NOW()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: CreateShowNoteReviewReply :one
INSERT INTO show_note_review_replies (
    thread_id, actor_kind, actor_user_id, actor_token_id, actor_name, body
) VALUES (
    sqlc.arg(thread_id), sqlc.arg(actor_kind), sqlc.narg(actor_user_id),
    sqlc.narg(actor_token_id), sqlc.arg(actor_name), sqlc.arg(body)
) RETURNING *;

-- name: ListShowNoteReviewReplies :many
SELECT * FROM show_note_review_replies
WHERE thread_id = sqlc.arg(thread_id)
ORDER BY created_at;

-- name: CreateShowNoteAgentLease :one
INSERT INTO show_note_agent_leases (
    show_note_id, api_token_id, user_id, agent_name, expires_at, last_cursor
) VALUES (
    sqlc.arg(show_note_id), sqlc.arg(api_token_id), sqlc.arg(user_id),
    sqlc.arg(agent_name), sqlc.arg(expires_at), sqlc.arg(last_cursor)
) RETURNING *;

-- name: RenewShowNoteAgentLease :one
UPDATE show_note_agent_leases
SET expires_at = sqlc.arg(expires_at), last_cursor = sqlc.arg(last_cursor), updated_at = NOW()
WHERE id = sqlc.arg(id) AND api_token_id = sqlc.arg(api_token_id)
RETURNING *;

-- name: GetShowNoteAgentLease :one
SELECT * FROM show_note_agent_leases
WHERE id = sqlc.arg(id) AND api_token_id = sqlc.arg(api_token_id)
  AND expires_at > NOW();

-- name: DeleteShowNoteAgentLease :exec
DELETE FROM show_note_agent_leases
WHERE id = sqlc.arg(id) AND api_token_id = sqlc.arg(api_token_id);

-- name: ListActiveShowNoteAgentLeases :many
SELECT * FROM show_note_agent_leases
WHERE show_note_id = sqlc.arg(show_note_id) AND expires_at > NOW()
ORDER BY created_at;
