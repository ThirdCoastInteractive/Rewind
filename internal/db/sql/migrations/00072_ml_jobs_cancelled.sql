-- +goose Up
ALTER TABLE ml_jobs DROP CONSTRAINT ml_jobs_status_check;
ALTER TABLE ml_jobs ADD CONSTRAINT ml_jobs_status_check CHECK(status IN (
    'queued','processing','paused','waiting_model','waiting_assets','retry_wait','succeeded','failed','superseded','cancelled'
));

-- Operator cancel, not pause: these jobs are out of line until an explicit retry.
UPDATE ml_jobs
SET status = 'cancelled',
    locked_at = NULL,
    locked_by = '',
    lease_token = NULL,
    updated_at = NOW()
WHERE status = 'paused';

-- +goose Down
UPDATE ml_jobs SET status = 'paused' WHERE status = 'cancelled';
ALTER TABLE ml_jobs DROP CONSTRAINT ml_jobs_status_check;
ALTER TABLE ml_jobs ADD CONSTRAINT ml_jobs_status_check CHECK(status IN (
    'queued','processing','paused','waiting_model','waiting_assets','retry_wait','succeeded','failed','superseded'
));
