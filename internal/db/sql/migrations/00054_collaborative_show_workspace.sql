-- +goose Up
-- Markdown-native collaborative show workspace. The legacy block tables are
-- intentionally retained as read-only recovery data after migration.

ALTER TABLE show_notes
    ADD COLUMN workspace_migrated_at TIMESTAMPTZ,
    ADD COLUMN workspace_migration_error TEXT NOT NULL DEFAULT '';

CREATE TABLE show_note_documents (
    show_note_id       UUID PRIMARY KEY REFERENCES show_notes(id) ON DELETE CASCADE,
    markdown           TEXT NOT NULL DEFAULT '',
    revision           BIGINT NOT NULL DEFAULT 0 CHECK (revision >= 0),
    snapshot           BYTEA NOT NULL DEFAULT ''::bytea,
    snapshot_revision  BIGINT NOT NULL DEFAULT 0 CHECK (snapshot_revision >= 0),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (snapshot_revision <= revision)
);

CREATE TABLE show_note_document_updates (
    show_note_id UUID NOT NULL REFERENCES show_notes(id) ON DELETE CASCADE,
    revision     BIGINT NOT NULL CHECK (revision > 0),
    update       BYTEA NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (show_note_id, revision)
);

CREATE INDEX show_note_document_updates_created_idx
    ON show_note_document_updates (show_note_id, created_at);

CREATE TABLE show_note_references (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    show_note_id   UUID NOT NULL REFERENCES show_notes(id) ON DELETE CASCADE,
    occurrence_key TEXT NOT NULL,
    ordinal        INTEGER NOT NULL CHECK (ordinal >= 0),
    kind           TEXT NOT NULL CHECK (kind IN ('video', 'clip', 'marker', 'external', 'unknown')),
    source_uri     TEXT NOT NULL,
    label          TEXT NOT NULL DEFAULT '',
    context        TEXT NOT NULL DEFAULT '',
    section_path   TEXT[] NOT NULL DEFAULT '{}',
    start_seconds  DOUBLE PRECISION,
    end_seconds    DOUBLE PRECISION,
    status         TEXT NOT NULL DEFAULT 'unresolved'
                   CHECK (status IN ('unresolved', 'resolving', 'ready', 'missing', 'unsupported', 'invalid', 'failed')),
    video_id       UUID REFERENCES videos(id) ON DELETE SET NULL,
    clip_id        UUID REFERENCES clips(id) ON DELETE SET NULL,
    marker_id      UUID REFERENCES markers(id) ON DELETE SET NULL,
    download_job_id UUID REFERENCES download_jobs(id) ON DELETE SET NULL,
    line_start     INTEGER NOT NULL CHECK (line_start > 0),
    line_end       INTEGER NOT NULL CHECK (line_end >= line_start),
    parsed_revision BIGINT NOT NULL CHECK (parsed_revision >= 0),
    diagnostic     TEXT NOT NULL DEFAULT '',
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (show_note_id, occurrence_key)
);

CREATE INDEX show_note_references_rundown_idx
    ON show_note_references (show_note_id, ordinal);
CREATE INDEX show_note_references_video_idx
    ON show_note_references (show_note_id, video_id) WHERE video_id IS NOT NULL;
CREATE INDEX show_note_references_download_job_idx
    ON show_note_references (download_job_id) WHERE download_job_id IS NOT NULL;

CREATE TABLE show_note_room_events (
    cursor          BIGSERIAL PRIMARY KEY,
    show_note_id    UUID NOT NULL REFERENCES show_notes(id) ON DELETE CASCADE,
    event_type      TEXT NOT NULL,
    actor_kind      TEXT NOT NULL DEFAULT 'human' CHECK (actor_kind IN ('human', 'agent', 'system')),
    actor_user_id   UUID REFERENCES users(id) ON DELETE SET NULL,
    actor_token_id  UUID REFERENCES api_tokens(id) ON DELETE SET NULL,
    actor_name      TEXT NOT NULL DEFAULT '',
    payload         JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX show_note_room_events_note_cursor_idx
    ON show_note_room_events (show_note_id, cursor);

-- +goose StatementBegin
CREATE FUNCTION notify_show_note_room_resource_updated() RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('show_note_room_events', NEW.show_note_id::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER show_note_room_resource_updated
    AFTER INSERT ON show_note_room_events
    FOR EACH ROW EXECUTE FUNCTION notify_show_note_room_resource_updated();

CREATE TABLE show_note_room_messages (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    show_note_id    UUID NOT NULL REFERENCES show_notes(id) ON DELETE CASCADE,
    event_cursor    BIGINT UNIQUE REFERENCES show_note_room_events(cursor) ON DELETE SET NULL,
    actor_kind      TEXT NOT NULL DEFAULT 'human' CHECK (actor_kind IN ('human', 'agent')),
    actor_user_id   UUID REFERENCES users(id) ON DELETE SET NULL,
    actor_token_id  UUID REFERENCES api_tokens(id) ON DELETE SET NULL,
    actor_name      TEXT NOT NULL DEFAULT '',
    body            TEXT NOT NULL CHECK (length(body) > 0),
    reply_to        UUID REFERENCES show_note_room_messages(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX show_note_room_messages_note_created_idx
    ON show_note_room_messages (show_note_id, created_at);

CREATE TABLE show_note_review_threads (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    show_note_id       UUID NOT NULL REFERENCES show_notes(id) ON DELETE CASCADE,
    kind               TEXT NOT NULL CHECK (kind IN ('comment', 'suggestion')),
    status             TEXT NOT NULL DEFAULT 'open'
                       CHECK (status IN ('open', 'resolved', 'accepted', 'rejected', 'stale')),
    actor_kind         TEXT NOT NULL DEFAULT 'human' CHECK (actor_kind IN ('human', 'agent')),
    actor_user_id      UUID REFERENCES users(id) ON DELETE SET NULL,
    actor_token_id     UUID REFERENCES api_tokens(id) ON DELETE SET NULL,
    actor_name         TEXT NOT NULL DEFAULT '',
    body               TEXT NOT NULL DEFAULT '',
    summary            TEXT NOT NULL DEFAULT '',
    base_revision      BIGINT NOT NULL CHECK (base_revision >= 0),
    base_markdown      TEXT NOT NULL DEFAULT '',
    expected_text      TEXT NOT NULL DEFAULT '',
    patch              TEXT NOT NULL DEFAULT '',
    anchor_start       BYTEA NOT NULL DEFAULT ''::bytea,
    anchor_end         BYTEA NOT NULL DEFAULT ''::bytea,
    start_line         INTEGER NOT NULL DEFAULT 1 CHECK (start_line > 0),
    start_column       INTEGER NOT NULL DEFAULT 1 CHECK (start_column > 0),
    end_line           INTEGER NOT NULL DEFAULT 1 CHECK (end_line > 0),
    end_column         INTEGER NOT NULL DEFAULT 1 CHECK (end_column > 0),
    detached           BOOLEAN NOT NULL DEFAULT FALSE,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    closed_at          TIMESTAMPTZ,
    closed_by_user_id  UUID REFERENCES users(id) ON DELETE SET NULL
);

CREATE INDEX show_note_review_threads_note_status_idx
    ON show_note_review_threads (show_note_id, status, created_at);

CREATE TABLE show_note_review_replies (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    thread_id       UUID NOT NULL REFERENCES show_note_review_threads(id) ON DELETE CASCADE,
    actor_kind      TEXT NOT NULL DEFAULT 'human' CHECK (actor_kind IN ('human', 'agent')),
    actor_user_id   UUID REFERENCES users(id) ON DELETE SET NULL,
    actor_token_id  UUID REFERENCES api_tokens(id) ON DELETE SET NULL,
    actor_name      TEXT NOT NULL DEFAULT '',
    body            TEXT NOT NULL CHECK (length(body) > 0),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE show_note_agent_leases (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    show_note_id    UUID NOT NULL REFERENCES show_notes(id) ON DELETE CASCADE,
    api_token_id    UUID NOT NULL REFERENCES api_tokens(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    agent_name      TEXT NOT NULL,
    expires_at      TIMESTAMPTZ NOT NULL,
    last_cursor     BIGINT NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX show_note_agent_leases_active_idx
    ON show_note_agent_leases (show_note_id, expires_at);

-- Clips did not previously carry a provenance key. This makes explicit
-- show-note materialization retry-safe without mutating reused library clips.
ALTER TABLE clips
    ADD COLUMN source TEXT NOT NULL DEFAULT 'user',
    ADD COLUMN source_ref TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX clips_source_ref_uidx
    ON clips (source, source_ref)
    WHERE source <> 'user' AND source_ref <> '';

-- +goose Down
DROP TRIGGER IF EXISTS show_note_room_resource_updated ON show_note_room_events;
DROP FUNCTION IF EXISTS notify_show_note_room_resource_updated();
DROP INDEX IF EXISTS clips_source_ref_uidx;
ALTER TABLE clips DROP COLUMN IF EXISTS source_ref;
ALTER TABLE clips DROP COLUMN IF EXISTS source;
DROP TABLE IF EXISTS show_note_agent_leases;
DROP TABLE IF EXISTS show_note_review_replies;
DROP TABLE IF EXISTS show_note_review_threads;
DROP TABLE IF EXISTS show_note_room_messages;
DROP TABLE IF EXISTS show_note_room_events;
DROP TABLE IF EXISTS show_note_references;
DROP TABLE IF EXISTS show_note_document_updates;
DROP TABLE IF EXISTS show_note_documents;
ALTER TABLE show_notes DROP COLUMN IF EXISTS workspace_migration_error;
ALTER TABLE show_notes DROP COLUMN IF EXISTS workspace_migrated_at;
