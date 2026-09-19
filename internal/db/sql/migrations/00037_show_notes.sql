-- +goose Up
-- Show Notes: nested, block-based content outlines built collaboratively by
-- producers. In producer v2 a show note IS the live session — it owns the
-- content (blocks), the host roster, the live flag, the public viewer code,
-- and the active scene state. Real-time multi-host editing is
-- server-authoritative; sync is via the in-process show-note broadcast hub
-- (see cmd/web/internal/shownote/hub.go).

CREATE TABLE show_notes (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title           TEXT NOT NULL DEFAULT 'Untitled',
    description     TEXT NOT NULL DEFAULT '',
    is_live         BOOLEAN NOT NULL DEFAULT FALSE,
    live_started_at TIMESTAMPTZ,
    -- public_code is the human-shareable viewer/OBS URL token, minted on go-live
    -- (Phase 3). NULL until live. UNIQUE allows many NULLs in Postgres.
    public_code     TEXT UNIQUE CHECK (public_code IS NULL OR length(public_code) = 6),
    -- scene_state holds the active composited-scene JSON (what player_sessions.state
    -- held in v1). Populated by the producer UI in Phase 3; '{}' until then.
    scene_state     JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_show_notes_owner ON show_notes (owner_id, updated_at DESC);

CREATE TABLE show_note_blocks (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    show_note_id      UUID NOT NULL REFERENCES show_notes(id) ON DELETE CASCADE,
    -- parent_id NULL = top level; self-FK cascades so deleting a section removes its subtree.
    parent_id         UUID REFERENCES show_note_blocks(id) ON DELETE CASCADE,
    block_type        TEXT NOT NULL CHECK (block_type IN ('section', 'video', 'clip', 'break')),
    title             TEXT NOT NULL DEFAULT '',
    notes             TEXT NOT NULL DEFAULT '',
    -- ON DELETE SET NULL: removing an archived video/clip degrades the block to
    -- "missing source" rather than blocking the delete or vanishing the block.
    video_id          UUID REFERENCES videos(id) ON DELETE SET NULL,
    clip_id           UUID REFERENCES clips(id) ON DELETE SET NULL,
    position          INTEGER NOT NULL DEFAULT 0,
    duration_override INTEGER,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Forbid cross-type refs; presence is enforced at creation, not here, so the
    -- SET NULL above can null a video/clip ref without violating this constraint.
    CONSTRAINT show_note_blocks_ref_per_type CHECK (
        (block_type IN ('section', 'break') AND video_id IS NULL AND clip_id IS NULL) OR
        (block_type = 'video' AND clip_id IS NULL) OR
        (block_type = 'clip'  AND video_id IS NULL)
    )
);

CREATE INDEX idx_show_note_blocks_tree ON show_note_blocks (show_note_id, parent_id, position);

CREATE TABLE show_note_hosts (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    show_note_id UUID NOT NULL REFERENCES show_notes(id) ON DELETE CASCADE,
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role         TEXT NOT NULL DEFAULT 'host' CHECK (role IN ('owner', 'host', 'viewer')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (show_note_id, user_id)
);

CREATE INDEX idx_show_note_hosts_user ON show_note_hosts (user_id);

-- +goose Down
DROP TABLE IF EXISTS show_note_hosts;
DROP TABLE IF EXISTS show_note_blocks;
DROP TABLE IF EXISTS show_notes;
