-- +goose Up
ALTER TABLE ml_jobs DROP CONSTRAINT ml_jobs_status_check;
ALTER TABLE ml_jobs ADD CONSTRAINT ml_jobs_status_check CHECK(status IN ('queued','processing','paused','waiting_model','waiting_assets','retry_wait','succeeded','failed','superseded'));

-- +goose Down
-- Keep superseded as a legal status when rolling application code back.
