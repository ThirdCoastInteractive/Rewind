package builtin

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/plugin"
)

// LocalML is the OSS ML plugin: enqueue writes ml_jobs, rewind-ml claims them.
type LocalML struct {
	DB *db.DatabaseConnection
}

func (m *LocalML) Enqueue(ctx context.Context, job plugin.Job) (string, error) {
	if m == nil || m.DB == nil {
		return "", fmt.Errorf("ml plugin has no database")
	}
	id, err := parseVideoUUID(job.VideoID)
	if err != nil {
		return "", fmt.Errorf("ml job video_id: %w", err)
	}
	q := m.DB.Queries(ctx)
	if job.Kind == plugin.KindTranscribe && (job.RequestKey != "" || job.RangeStart != nil || job.RangeEnd != nil) {
		key := job.RequestKey
		if key == "" {
			key = "full"
		}
		row, err := q.EnqueueTranscription(ctx, &db.EnqueueTranscriptionParams{
			VideoID:    id,
			RequestKey: key,
			RangeStart: job.RangeStart,
			RangeEnd:   job.RangeEnd,
		})
		if err != nil {
			return "", err
		}
		return uuid.UUID(row.ID.Bytes).String(), nil
	}
	if err := q.EnqueueMLJob(ctx, &db.EnqueueMLJobParams{
		VideoID:        id,
		Kind:           job.Kind,
		Priority:       job.Priority,
		TranscriptHash: job.TranscriptHash,
		ModelDigest:    job.ModelDigest,
		PromptVersion:  job.PromptVersion,
	}); err != nil {
		return "", err
	}
	_ = q.RequeueMLJob(ctx, &db.RequeueMLJobParams{
		VideoID:        id,
		Kind:           job.Kind,
		TranscriptHash: job.TranscriptHash,
	})
	return "", nil
}

func parseVideoUUID(s string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(s); err != nil {
		return id, err
	}
	if !id.Valid {
		return id, fmt.Errorf("invalid uuid %q", s)
	}
	return id, nil
}
