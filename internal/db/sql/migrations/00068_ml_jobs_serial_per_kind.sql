-- +goose Up
-- Visual indexing, transcription, and context windows use different devices.
-- Serialise each kind, not the whole ML queue.
DROP INDEX IF EXISTS ml_jobs_serial_processing_uidx;
CREATE UNIQUE INDEX ml_jobs_serial_processing_uidx ON ml_jobs (kind)
    WHERE status = 'processing';

-- +goose Down
DROP INDEX IF EXISTS ml_jobs_serial_processing_uidx;
CREATE UNIQUE INDEX ml_jobs_serial_processing_uidx ON ml_jobs ((TRUE))
    WHERE status = 'processing';
