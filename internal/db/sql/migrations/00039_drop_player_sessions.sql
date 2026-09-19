-- +goose Up
-- Retire the standalone player_sessions model. In producer v2 the show note IS
-- the session: the active scene lives on show_notes.scene_state, the public
-- viewer code on show_notes.public_code, and live host presence in
-- producer_connections. Verified to have no remaining FK dependents.
-- Keep legacy rows for release upgrades. New code no longer uses this table,
-- but retiring a feature must not destroy the user's saved producer state.
SELECT 1;

-- +goose Down
-- Best-effort recreation of the original table (data is not restored).
CREATE TABLE IF NOT EXISTS player_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_code TEXT NOT NULL UNIQUE CHECK (length(session_code) = 6),
    producer_id UUID NOT NULL,
    current_video_id UUID REFERENCES videos(id),
    state JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    last_activity TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_player_sessions_code ON player_sessions(session_code);
CREATE INDEX IF NOT EXISTS idx_player_sessions_expires ON player_sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_player_sessions_producer ON player_sessions(producer_id);
