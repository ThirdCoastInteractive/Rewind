// Package ingest moves downloaded media into the archive. Derived playback
// assets (preview, seek sprites, waveform) run on a separate worker pool.
package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"thirdcoast.systems/rewind/internal/comments"
	"thirdcoast.systems/rewind/internal/config"
	"thirdcoast.systems/rewind/internal/creatorlink"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/runtimecfg"
)

// assetCatchupLockID is a session-level advisory lock so only one ingest
// process runs asset catchup / probe backfill at a time.
const assetCatchupLockID int64 = 76001

const (
	ingestWorkerSlots      = 32
	ingestIdlePoll         = 30 * time.Second
	assetCatchupBusyPoll   = 30 * time.Second
	assetCatchupIdlePoll   = 8 * time.Minute
	linkHarvestPoll        = 30 * time.Second
	creatorlinkPoll        = 20 * time.Second
	commenterIdentityPoll  = 1 * time.Minute
	commenterIdentityBatch = int32(5)
	stuckJobRecoveryPoll   = 2 * time.Minute
)

// Start runs ingest workers, asset workers, and background loops until ctx is cancelled.
// INGEST_WORKERS / ASSET_WORKERS, whisper enqueue, and spool/downloads paths are read from env.
func Start(ctx context.Context, dbc *db.DatabaseConnection, conf *config.Config) error {
	if dbc == nil {
		return fmt.Errorf("ingest: nil database connection")
	}
	if conf == nil {
		return fmt.Errorf("ingest: nil config")
	}

	if err := runtimecfg.Start(ctx, dbc, "ingest"); err != nil {
		return fmt.Errorf("ingest settings: %w", err)
	}

	slog.Info("ingest paths",
		"downloads", downloadsDir(),
		"spool", spoolDir(),
		"whisper_enqueue", "ml_jobs.transcribe",
	)

	slog.Info("Recovering stuck ingest jobs from previous service instances")
	if err := dbc.Queries(ctx).RecoverStuckIngestJobs(ctx); err != nil {
		slog.Error("failed to recover stuck ingest jobs", "error", err)
	}
	if n, err := dbc.Queries(ctx).FailExcessiveRetryIngestJobs(ctx); err != nil {
		slog.Error("failed to fail excessive retry ingest jobs", "error", err)
	} else if n > 0 {
		slog.Warn("permanently failed ingest jobs exceeding max retries", "count", n)
	}

	go recoverStuckIngestJobsLoop(ctx, dbc)

	workers := envInt("INGEST_WORKERS", 2)
	assetWorkers := envInt("ASSET_WORKERS", 2)
	wake := make(chan struct{}, 1)
	signalWake := func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	go db.RunListenLoop(ctx, dbc, []string{"ingest_jobs"}, func(*pgconn.Notification) {
		signalWake()
	}, signalWake)

	// processing.ingest_workers / processing.asset_workers are the live caps;
	// surplus slots park in WaitWorker.
	slog.Info("Ingest workers started", "ingest_workers", workers, "asset_workers", assetWorkers, "slots", ingestWorkerSlots)
	for i := 0; i < ingestWorkerSlots; i++ {
		go ingestWorker(ctx, dbc, wake, i)
		go assetWorker(ctx, dbc, wake, i)
	}

	go runAssetCatchupLoop(ctx, dbc)
	go runLinkHarvestLoop(ctx, dbc)
	go runCreatorlinkLoop(ctx, dbc)
	go runCommenterIdentityLoop(ctx, dbc)

	<-ctx.Done()
	slog.Info("Ingest service stopping")
	return nil
}

func recoverStuckIngestJobsLoop(ctx context.Context, dbc *db.DatabaseConnection) {
	ticker := time.NewTicker(stuckJobRecoveryPoll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := dbc.Queries(ctx).RecoverStuckIngestJobs(ctx); err != nil {
				slog.Error("periodic: failed to recover stuck ingest jobs", "error", err)
			}
			if n, err := dbc.Queries(ctx).FailExcessiveRetryIngestJobs(ctx); err != nil {
				slog.Error("periodic: failed to fail excessive retry ingest jobs", "error", err)
			} else if n > 0 {
				slog.Warn("periodic: permanently failed ingest jobs exceeding max retries", "count", n)
			}
		}
	}
}

func runAssetCatchupLoop(ctx context.Context, dbc *db.DatabaseConnection) {
	conn, err := dbc.Acquire(ctx)
	if err != nil {
		slog.Warn("asset catchup: acquire connection failed", "error", err)
		return
	}
	defer conn.Release()

	q := db.New(conn)
	acquired, err := q.TryAdvisoryLock(ctx, assetCatchupLockID)
	if err != nil {
		slog.Warn("asset catchup lock error", "error", err)
		return
	}
	if !acquired {
		slog.Info("asset catchup skipped; another process holds the lock")
		return
	}
	defer func() {
		_, _ = q.AdvisoryUnlock(context.Background(), assetCatchupLockID)
	}()

	ctx = withSharedFFmpeg(ctx)
	recoverOrphanedVideoPaths(ctx, dbc)
	runProbeBackfill(ctx, dbc)

	interval := assetCatchupBusyPoll
	for {
		if ctx.Err() != nil {
			return
		}
		runRemoteMasterCatchup(ctx, dbc)
		n := runAssetCatchupUnit(ctx, dbc)
		if n == 0 {
			interval = assetCatchupIdlePoll
		} else {
			interval = assetCatchupBusyPoll
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

func runLinkHarvestLoop(ctx context.Context, dbc *db.DatabaseConnection) {
	ticker := time.NewTicker(linkHarvestPoll)
	defer ticker.Stop()
	for {
		runLinkHarvestUnit(ctx, dbc)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runCreatorlinkLoop(ctx context.Context, dbc *db.DatabaseConnection) {
	ticker := time.NewTicker(creatorlinkPoll)
	defer ticker.Stop()
	for {
		creatorlink.Apply(ctx, dbc.Queries(ctx))
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runCommenterIdentityLoop(ctx context.Context, dbc *db.DatabaseConnection) {
	ticker := time.NewTicker(commenterIdentityPoll)
	defer ticker.Stop()
	for {
		n, err := comments.RunIdentityBackfill(ctx, dbc, commenterIdentityBatch)
		if err != nil {
			slog.Warn("commenter identity backfill error", "error", err)
		} else if n > 0 {
			slog.Info("commenter identity backfill", "videos", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
