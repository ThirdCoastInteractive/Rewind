-- +goose Up
ALTER TABLE stitch_projects
    ADD COLUMN description TEXT NOT NULL DEFAULT '',
    ADD COLUMN tags TEXT[] NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE stitch_projects
    DROP COLUMN IF EXISTS tags,
    DROP COLUMN IF EXISTS description;
