-- +goose Up
CREATE TABLE player_scene_presets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    producer_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    scene JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (producer_id, name)
);

CREATE INDEX idx_player_scene_presets_producer ON player_scene_presets(producer_id);
CREATE INDEX idx_player_scene_presets_updated ON player_scene_presets(updated_at);

-- +goose Down
DROP TABLE IF EXISTS player_scene_presets;
