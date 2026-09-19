package stitch

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"strings"
	"thirdcoast.systems/rewind/internal/db"
	"time"
)

// AlignmentClaim is the immutable payload claimed by an alignment worker.
type AlignmentClaim struct {
	AlignmentJob
	OwnerID       string
	ProjectID     string
	Text          string
	SourceVideoID string
	SourceStartUS int64
	SourceEndUS   int64
	StartUS       int64
	EndUS         int64
}

// ClaimAlignment atomically claims one pending alignment job, or returns nil when empty.
func ClaimAlignment(ctx context.Context, dbc *db.DatabaseConnection, workerID string) (*AlignmentClaim, error) {
	var c AlignmentClaim
	var result []byte
	err := dbc.QueryRow(ctx, `WITH expired AS (UPDATE stitch_alignment_jobs SET status='pending',locked_by='',updated_at=now() WHERE status='processing' AND updated_at < now()-interval '10 minutes' RETURNING id), next AS (SELECT id FROM stitch_alignment_jobs WHERE status='pending' ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1) UPDATE stitch_alignment_jobs j SET status='processing',locked_by=$1,updated_at=now() WHERE j.id=(SELECT id FROM next) RETURNING j.id,j.caption_id,j.status,j.language,j.model_version,j.alignment_key,j.owner_id::text,j.project_id::text,j.text,j.source_video_id,j.source_start_us,j.source_end_us,j.start_us,j.end_us,j.result`, workerID).Scan(&c.ID, &c.CaptionID, &c.Status, &c.Language, &c.ModelVersion, &c.AlignmentKey, &c.OwnerID, &c.ProjectID, &c.Text, &c.SourceVideoID, &c.SourceStartUS, &c.SourceEndUS, &c.StartUS, &c.EndUS, &result)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// FinishAlignmentJob persists a worker result without changing the editor document.
func FinishAlignmentJob(ctx context.Context, dbc *db.DatabaseConnection, id interface{}, status string, result any, workerError string) error {
	b, e := json.Marshal(result)
	if e != nil {
		return e
	}
	_, e = dbc.Exec(ctx, `UPDATE stitch_alignment_jobs SET status=$2,result=$3,error=$4,locked_by='',updated_at=now() WHERE id=$1`, id, status, b, workerError)
	return e
}

// RunAlignmentWorker polls queued jobs until ctx is cancelled. Polling is the
// recovery path; database notifications may be added as a wake-up optimization.
func RunAlignmentWorker(ctx context.Context, dbc *db.DatabaseConnection, workerID string, poll time.Duration, run func(context.Context, *AlignmentClaim) (any, error), apply func(context.Context, *AlignmentClaim, any) error) error {
	if poll <= 0 {
		poll = 2 * time.Second
	}
	for {
		claim, err := ClaimAlignment(ctx, dbc, workerID)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		if claim != nil {
			result, runErr := run(ctx, claim)
			if runErr != nil {
				if err := FinishAlignmentJob(ctx, dbc, claim.ID, "error", map[string]any{"state": "error"}, runErr.Error()); err != nil {
					return err
				}
				continue
			}
			if err := apply(ctx, claim, result); err != nil {
				if strings.Contains(err.Error(), "stale alignment result") {
					continue
				}
				return err
			}
			continue
		}
		t := time.NewTimer(poll)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}
