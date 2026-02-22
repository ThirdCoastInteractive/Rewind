-- +goose Up
CREATE TABLE ytdlp_logs (
    id BIGSERIAL PRIMARY KEY,
    job_id UUID NOT NULL REFERENCES download_jobs(id) ON DELETE CASCADE,
    stream log_stream NOT NULL,
    message TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_ytdlp_logs_job_id ON ytdlp_logs(job_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS ytdlp_logs;
