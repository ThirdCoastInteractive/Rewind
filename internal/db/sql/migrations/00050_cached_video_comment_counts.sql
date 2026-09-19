-- +goose Up

-- Creator reports and video headers read comment totals frequently. Counting
-- directly from video_comments is especially expensive while comment ingest is
-- active because PostgreSQL cannot use an index-only count until vacuum catches
-- up with the newly-written heap pages.
ALTER TABLE videos ADD COLUMN comment_count BIGINT NOT NULL DEFAULT 0;

UPDATE videos v
SET comment_count = counts.total
FROM (
    SELECT video_id, COUNT(*)::bigint AS total
    FROM video_comments
    GROUP BY video_id
) counts
WHERE counts.video_id = v.id;

-- +goose Down
ALTER TABLE videos DROP COLUMN IF EXISTS comment_count;
