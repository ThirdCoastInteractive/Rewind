-- +goose Up
-- video_comments is a wide heap (search tsvector + raw JSONB). Default
-- autovacuum scale (20%) never fired here because reltuples stayed inflated
-- without ANALYZE, so COUNT(*) and index-only scans heap-fetched for seconds.
ALTER TABLE video_comments SET (
    autovacuum_vacuum_scale_factor = 0.02,
    autovacuum_analyze_scale_factor = 0.01,
    autovacuum_vacuum_threshold = 200,
    autovacuum_analyze_threshold = 200
);

-- +goose Down
ALTER TABLE video_comments RESET (
    autovacuum_vacuum_scale_factor,
    autovacuum_analyze_scale_factor,
    autovacuum_vacuum_threshold,
    autovacuum_analyze_threshold
);
