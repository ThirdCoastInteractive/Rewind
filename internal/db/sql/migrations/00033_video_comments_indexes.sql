-- +goose Up
-- Partial index supporting ListVideoComments: top-level comments for a video,
-- pre-sorted by like_count then published_at. Lets the paginated list use an
-- index scan + LIMIT instead of sorting all comments for the video (the cold
-- top-level sort was ~800ms on a 9k-comment video).
CREATE INDEX IF NOT EXISTS video_comments_toplevel_rank_idx
    ON video_comments (video_id, like_count DESC NULLS LAST, published_at DESC NULLS LAST)
    WHERE parent_id IS NULL OR parent_id = 'root';

-- Index supporting reply lookups and the per-comment reply_count subquery.
CREATE INDEX IF NOT EXISTS video_comments_parent_idx
    ON video_comments (video_id, parent_id)
    WHERE parent_id IS NOT NULL AND parent_id <> 'root';

-- +goose Down
DROP INDEX IF EXISTS video_comments_toplevel_rank_idx;
DROP INDEX IF EXISTS video_comments_parent_idx;
