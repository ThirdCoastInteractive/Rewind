package ingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/runtimecfg"
)

// startHeartbeat begins a background goroutine that periodically touches updated_at
// on a processing ingest job. This prevents the recovery goroutine from resetting
// long-running asset jobs back to "queued".
// Returns a cancel function that must be called when processing finishes.
func startHeartbeat(ctx context.Context, q *db.Queries, jobID pgtype.UUID) context.CancelFunc {
	hbCtx, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-ticker.C:
				if err := q.HeartbeatIngestJob(hbCtx, jobID); err != nil {
					if hbCtx.Err() == nil {
						slog.Warn("heartbeat failed for ingest job", "job_id", jobID, "error", err)
					}
				}
			}
		}
	}()
	return cancel
}

func ingestWorker(ctx context.Context, dbc *db.DatabaseConnection, wake <-chan struct{}, index int) {
	claimWorker(ctx, dbc, wake, index, "processing.ingest_workers", false)
}

func assetWorker(ctx context.Context, dbc *db.DatabaseConnection, wake <-chan struct{}, index int) {
	claimWorker(ctx, dbc, wake, index, "processing.asset_workers", true)
}

func claimWorker(ctx context.Context, dbc *db.DatabaseConnection, wake <-chan struct{}, index int, workerKey string, assetJobs bool) {
	q := dbc.Queries(ctx)
	kind := "ingest"
	if assetJobs {
		kind = "assets"
	}

	for {
		if ctx.Err() != nil {
			return
		}

		for {
			if !runtimecfg.WaitWorker(ctx, workerKey, index) {
				return
			}
			job, err := q.DequeueIngestJob(ctx, assetJobs)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					break
				}
				slog.Error("failed to dequeue ingest job", "kind", kind, "error", err)
				time.Sleep(2 * time.Second)
				break
			}

			jobCtx, captureErr := runtimecfg.Job(ctx, dbc, "ingest", job.IngestJobID)
			if captureErr != nil {
				message := captureErr.Error()
				_ = q.MarkIngestJobFailed(ctx, &db.MarkIngestJobFailedParams{ID: job.IngestJobID, LastError: &message})
				continue
			}
			jobCtx = withSharedFFmpeg(jobCtx)
			// Asset jobs hold the heartbeat while seek/waveform/preview run.
			// Ingest jobs finish after publish, so this is usually brief.
			stopHeartbeat := startHeartbeat(ctx, q, job.IngestJobID)

			func() {
				defer stopHeartbeat()
				defer func() {
					if r := recover(); r != nil {
						errMsg := fmt.Sprintf("panic: %v", r)
						slog.Error("ingest job panicked", "kind", kind, "ingest_job_id", job.IngestJobID, "panic", r)
						_ = q.MarkIngestJobFailed(ctx, &db.MarkIngestJobFailedParams{ID: job.IngestJobID, LastError: &errMsg})
					}
				}()

				var runErr error
				if assetJobs {
					runErr = processAssetRegenerationJob(jobCtx, q, job)
				} else {
					runErr = processIngestJob(jobCtx, q, job)
				}
				if runErr != nil {
					slog.Error("ingest job failed", "kind", kind, "ingest_job_id", job.IngestJobID, "error", runErr)
					errMsg := runErr.Error()
					_ = q.MarkIngestJobFailed(ctx, &db.MarkIngestJobFailedParams{ID: job.IngestJobID, LastError: &errMsg})
				}
			}()
		}

		select {
		case <-ctx.Done():
			return
		case <-wake:
		case <-time.After(ingestIdlePoll):
		}
	}
}
