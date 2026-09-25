-- +goose NO TRANSACTION

-- +goose Up

-- The network page uses commenter_id as the lookup key for these correlated
-- subqueries.  The existing primary keys start with user_id or a_id/b_id and
-- force repeated scans as the commenter table grows.
CREATE INDEX CONCURRENTLY IF NOT EXISTS commenter_watchlist_commenter_user_idx
    ON commenter_watchlist (commenter_id, user_id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS osint_flags_open_commenter_kind_idx
    ON osint_flags (commenter_id, kind)
    WHERE dismissed_at IS NULL AND commenter_id IS NOT NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS commenter_links_a_kind_idx
    ON commenter_links (a_id, kind);
CREATE INDEX CONCURRENTLY IF NOT EXISTS commenter_links_b_kind_idx
    ON commenter_links (b_id, kind);

-- +goose Down

DROP INDEX CONCURRENTLY IF EXISTS commenter_links_b_kind_idx;
DROP INDEX CONCURRENTLY IF EXISTS commenter_links_a_kind_idx;
DROP INDEX CONCURRENTLY IF EXISTS osint_flags_open_commenter_kind_idx;
DROP INDEX CONCURRENTLY IF EXISTS commenter_watchlist_commenter_user_idx;
