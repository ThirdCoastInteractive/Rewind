-- +goose Up
CREATE TABLE playback_positions (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    video_id UUID NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    position_seconds DOUBLE PRECISION NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, video_id)
);

CREATE INDEX idx_playback_positions_user_id ON playback_positions(user_id);
CREATE INDEX idx_playback_positions_video_id ON playback_positions(video_id);
CREATE INDEX idx_playback_positions_updated_at ON playback_positions(updated_at);

-- +goose Down
DROP TABLE IF EXISTS playback_positions;
