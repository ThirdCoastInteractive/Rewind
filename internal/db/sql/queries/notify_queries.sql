-- Listen for ingest job notifications.
-- name: ListenIngestJobs :exec
LISTEN ingest_jobs;

-- Listen for download job notifications.
-- name: ListenDownloadJobs :exec
LISTEN download_jobs;
