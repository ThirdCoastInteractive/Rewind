-- +goose Up
-- producer_connections tracks who is currently live (connected via WebRTC) on a
-- show note and which host holds the "director" role (single playback controller).
-- Ephemeral: rows are inserted on join, heartbeated via last_ping, and removed on
-- disconnect or when stale. Keyed by show_note_id (the show note IS the session).
CREATE TABLE producer_connections (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    show_note_id UUID NOT NULL REFERENCES show_notes(id) ON DELETE CASCADE,
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    is_director  BOOLEAN NOT NULL DEFAULT FALSE,
    connected_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_ping    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (show_note_id, user_id)
);

CREATE INDEX idx_producer_connections_show ON producer_connections (show_note_id);
CREATE INDEX idx_producer_connections_director ON producer_connections (show_note_id, is_director);

-- +goose Down
DROP TABLE IF EXISTS producer_connections;
