-- +goose Up
CREATE TABLE player_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_code TEXT NOT NULL UNIQUE CHECK (length(session_code) = 6),
    producer_id UUID NOT NULL,
    current_video_id UUID REFERENCES videos(id),
    state JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    last_activity TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_player_sessions_code ON player_sessions(session_code);
CREATE INDEX idx_player_sessions_expires ON player_sessions(expires_at);
CREATE INDEX idx_player_sessions_producer ON player_sessions(producer_id);

-- +goose Down
DROP TABLE IF EXISTS player_sessions;
