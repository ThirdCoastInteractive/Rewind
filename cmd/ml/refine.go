package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"thirdcoast.systems/rewind/cmd/ml/internal/mlcore"
	"thirdcoast.systems/rewind/internal/db"
)

func handleRefineBoundaries(ctx context.Context, dbc *db.DatabaseConnection, job *db.MlJob) error {
	q, tx, err := dbc.NewWithTX(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = q.LockMLPublication(ctx, &db.LockMLPublicationParams{ID: job.ID, LeaseToken: job.LeaseToken}); err != nil {
		return leaseLost(err)
	}
	if err = q.LockTranscriptForContext(ctx, job.VideoID); err != nil {
		return err
	}
	hash, err := q.GetTranscriptFingerprint(ctx, job.VideoID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("waiting_assets: transcript fingerprint missing")
		}
		return fmt.Errorf("transcript fingerprint: %w", err)
	}
	if job.TranscriptHash != "" && hash != job.TranscriptHash {
		return mlcore.ErrSuperseded
	}
	capCues, _, err := loadTranscriptCues(ctx, q, job.VideoID)
	if err != nil {
		return nil // nothing to snap; succeed
	}
	cues := mlcore.CuesFromCaptions(capCues)
	if len(cues) == 0 {
		return nil
	}
	rows, err := q.ListContextWindowsByVideo(ctx, job.VideoID)
	if err != nil {
		return fmt.Errorf("list windows: %w", err)
	}
	for _, r := range rows {
		if r == nil || r.Stale || r.OverrideBounds || r.Origin != "generated" {
			continue
		}
		w := mlcore.Window{Start: r.StartTs, End: r.EndTs, CueStart: int(r.CueStart), CueEnd: int(r.CueEnd)}
		snapped := mlcore.SnapToCues(w, cues)
		if snapped.Start == r.StartTs && snapped.End == r.EndTs {
			continue
		}
		if err := q.UpdateContextWindowBounds(ctx, &db.UpdateContextWindowBoundsParams{
			ID:       r.ID,
			StartTs:  snapped.Start,
			EndTs:    snapped.End,
			CueStart: int32(snapped.CueStart),
			CueEnd:   int32(snapped.CueEnd),
		}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
