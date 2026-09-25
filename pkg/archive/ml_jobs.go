package archive

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"thirdcoast.systems/rewind/internal/db"
)

// ClaimedMLJob is one ml_jobs row locked for an external worker.
type ClaimedMLJob struct {
	ID, Lease, VideoID, Kind, Checkpoint string
	Attempts                             int32
}

// claimParkedMLJobSQL matches ClaimMLJob's lock and attempt rules but does not
// read ml_runtime_health, so a kind parked for local workers can still be claimed.
const claimParkedMLJobSQL = `
UPDATE ml_jobs
SET status = 'processing',
    locked_at = NOW(),
    locked_by = $1,
    lease_token = gen_random_uuid(),
    attempts = CASE WHEN status IN ('queued', 'retry_wait') THEN attempts + 1 ELSE attempts END,
    updated_at = NOW()
WHERE id = (
    SELECT j.id
    FROM ml_jobs j
    WHERE j.kind = ANY($2::text[])
      AND j.status IN ('queued', 'waiting_model', 'waiting_assets', 'retry_wait')
      AND j.retry_at <= now()
    ORDER BY j.priority, j.created_at
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING id::text, lease_token::text, video_id::text, kind, checkpoint::text, attempts
`

const parkMLKindSQL = `
INSERT INTO ml_runtime_health (kind, last_error, retry_at)
VALUES ($1, 'workers-ai', now() + interval '365 days')
ON CONFLICT (kind) DO UPDATE SET
    last_error = EXCLUDED.last_error,
    retry_at = EXCLUDED.retry_at,
    updated_at = now()
`

// retry_at is the caller-supplied time, not now()+seconds. model_digest is left unchanged.
const finishMLJobSQL = `
UPDATE ml_jobs
SET status = $3,
    failure_count = CASE
        WHEN $3::text IN ('failed', 'retry_wait') THEN failure_count + 1
        WHEN $3::text IN ('succeeded', 'superseded') THEN 0
        ELSE failure_count
    END,
    last_error = $4,
    locked_at = NULL,
    locked_by = '',
    retry_at = $5,
    updated_at = NOW()
WHERE id = $1
  AND lease_token = $2
  AND status = 'processing'
`

const releaseDailyLimitSQL = `
UPDATE ml_jobs
SET retry_at = now(),
    updated_at = now()
WHERE last_error = 'daily_limit'
  AND status IN ('waiting_model', 'queued', 'retry_wait')
  AND retry_at > now() + interval '15 minutes'
`

// ReleaseDailyLimitJobs pulls quota waits that are parked well past a short
// retry forward to now. A paid plan can then claim them without waiting for
// the next UTC midnight. A wait of fifteen minutes or less is left alone.
func ReleaseDailyLimitJobs(ctx context.Context) error {
	dbc, err := boundDB()
	if err != nil {
		return err
	}
	_, err = dbc.Exec(ctx, releaseDailyLimitSQL)
	return err
}

// ParkMLKinds upserts ml_runtime_health so ClaimMLJob skips each kind until
// retry_at (now()+365 days). last_error is workers-ai.
func ParkMLKinds(ctx context.Context, kinds []string) error {
	dbc, err := boundDB()
	if err != nil {
		return err
	}
	var list []string
	for _, kind := range kinds {
		kind = strings.TrimSpace(kind)
		if kind == "" {
			continue
		}
		list = append(list, kind)
	}
	if len(list) == 0 {
		return nil
	}
	tx, err := dbc.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, kind := range list {
		if _, err := tx.Exec(ctx, parkMLKindSQL, kind); err != nil {
			return fmt.Errorf("archive: park ml kind %s: %w", kind, err)
		}
	}
	return tx.Commit(ctx)
}

// reapStaleMLJobsSQL is RecoverStuckMLJobs. A crashed worker leaves the row
// processing, and ClaimParkedMLJob does not otherwise select that status.
// HeartbeatMLJob refreshes locked_at, so a live lease is not taken.
const reapStaleMLJobsSQL = `
UPDATE ml_jobs
SET status = 'queued',
    locked_at = NULL,
    locked_by = '',
    updated_at = NOW()
WHERE status = 'processing'
  AND (locked_at IS NULL OR locked_at < NOW() - INTERVAL '15 minutes')
`

// ClaimParkedMLJob claims one ready job of the given kinds.
// It does not consult ml_runtime_health. No ready row returns (nil, nil).
// A processing row whose lock is older than 15 minutes is queued again first.
func ClaimParkedMLJob(ctx context.Context, workerID string, kinds []string) (*ClaimedMLJob, error) {
	dbc, err := boundDB()
	if err != nil {
		return nil, err
	}
	if len(kinds) == 0 {
		return nil, nil
	}
	if _, err := dbc.Exec(ctx, reapStaleMLJobsSQL); err != nil {
		return nil, err
	}
	var job ClaimedMLJob
	err = dbc.QueryRow(ctx, claimParkedMLJobSQL, workerID, kinds).Scan(
		&job.ID,
		&job.Lease,
		&job.VideoID,
		&job.Kind,
		&job.Checkpoint,
		&job.Attempts,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// HeartbeatMLJob refreshes the processing lease. A missing row or wrong lease is an error.
func HeartbeatMLJob(ctx context.Context, id, lease string) error {
	dbc, err := boundDB()
	if err != nil {
		return err
	}
	jobID, err := parsePGUUID(id)
	if err != nil {
		return err
	}
	leaseID, err := parsePGUUID(lease)
	if err != nil {
		return err
	}
	n, err := dbc.Queries(ctx).HeartbeatMLJob(ctx, &db.HeartbeatMLJobParams{ID: jobID, LeaseToken: leaseID})
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("archive: ml job not processing for this lease")
	}
	return nil
}

// FinishMLJob completes a processing job that still holds lease.
// retryAt is stored as given. failed and retry_wait increment failure_count;
// succeeded and superseded zero it. model_digest is not changed.
func FinishMLJob(ctx context.Context, id, lease, status, lastError string, retryAt time.Time) error {
	dbc, err := boundDB()
	if err != nil {
		return err
	}
	jobID, err := parsePGUUID(id)
	if err != nil {
		return err
	}
	leaseID, err := parsePGUUID(lease)
	if err != nil {
		return err
	}
	tag, err := dbc.Exec(ctx, finishMLJobSQL, jobID, leaseID, status, lastError, retryAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("archive: ml job not processing for this lease")
	}
	return nil
}
