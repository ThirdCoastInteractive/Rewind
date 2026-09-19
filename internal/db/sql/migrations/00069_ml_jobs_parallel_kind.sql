-- +goose Up
-- Allow up to ml.concurrent processing rows per kind. ClaimMLJob enforces the
-- count; a unique (kind) index made a second transcribe/context/visual job
-- impossible even when the GPU had room.
DROP INDEX IF EXISTS ml_jobs_serial_processing_uidx;

-- +goose Down
CREATE UNIQUE INDEX ml_jobs_serial_processing_uidx ON ml_jobs (kind)
    WHERE status = 'processing';
