-- +goose Up
ALTER TABLE clips ADD COLUMN IF NOT EXISTS shot_list JSONB DEFAULT '[]';

-- +goose Down
ALTER TABLE clips DROP COLUMN IF EXISTS shot_list;
