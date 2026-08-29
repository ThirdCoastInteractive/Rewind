-- +goose Up
-- Global download quality cap, in vertical pixels (e.g. 720, 1080, 2160).
-- 0 means no cap: download the best available rendition.
ALTER TABLE instance_settings
    ADD COLUMN max_download_height INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE instance_settings
    DROP COLUMN max_download_height;
