package contextwindow

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"math"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/plugin"
)

// PromptVersion is the context-generation prompt identity shared with the SQL trigger.
const PromptVersion = "context-v5-topics"

// Validate checks bounds against the video duration and requires a title.
func Validate(start, end float64, title string, durationSeconds *int32) error {
	if math.IsNaN(start) || math.IsNaN(end) || math.IsInf(start, 0) || math.IsInf(end, 0) || start < 0 || end <= start || strings.TrimSpace(title) == "" || (durationSeconds != nil && end > float64(*durationSeconds)) {
		return fmt.Errorf("invalid context window bounds or title")
	}
	return nil
}

// Enqueue queues context-window generation for the current transcript fingerprint.
func Enqueue(ctx context.Context, dbc *db.DatabaseConnection, videoID pgtype.UUID) error {
	q := dbc.Queries(ctx)
	transcript, err := q.GetVideoTranscript(ctx, videoID)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("generate captions first: this video has no transcript")
	}
	if err != nil {
		return err
	}
	if strings.TrimSpace(transcript.Text) == "" {
		return fmt.Errorf("generate captions first: this video's transcript is empty")
	}
	hash, err := q.GetTranscriptFingerprint(ctx, videoID)
	if err != nil {
		return err
	}
	if err = plugin.Enqueue(ctx, plugin.Job{
		VideoID: uuid.UUID(videoID.Bytes).String(), Kind: plugin.KindContext, Priority: 100,
		TranscriptHash: hash, PromptVersion: PromptVersion,
	}); err != nil {
		return err
	}
	if err := q.PromoteContextJobs(ctx, &db.PromoteContextJobsParams{VideoID: videoID, TranscriptHash: hash}); err != nil {
		return err
	}
	return q.SupersedeStaleContextJobs(ctx, &db.SupersedeStaleContextJobsParams{VideoID: videoID, TranscriptHash: hash})
}

// EnqueueIfMissing queues context for a video the user is watching without
// re-running a succeeded generation for the current transcript.
func EnqueueIfMissing(ctx context.Context, dbc *db.DatabaseConnection, videoID pgtype.UUID) error {
	q := dbc.Queries(ctx)
	transcript, err := q.GetVideoTranscript(ctx, videoID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if strings.TrimSpace(transcript.Text) == "" {
		return nil
	}
	hash, err := q.GetTranscriptFingerprint(ctx, videoID)
	if err != nil {
		return err
	}
	sets, err := q.ListContextWindowSetsForVideo(ctx, videoID)
	if err != nil {
		return err
	}
	for _, set := range sets {
		if set != nil && set.TranscriptHash == hash && set.Status == "succeeded" && strings.HasPrefix(set.PromptVersion, PromptVersion) {
			return q.SupersedeStaleContextJobs(ctx, &db.SupersedeStaleContextJobsParams{VideoID: videoID, TranscriptHash: hash})
		}
	}
	if err = plugin.Enqueue(ctx, plugin.Job{
		VideoID: uuid.UUID(videoID.Bytes).String(), Kind: plugin.KindContext, Priority: 100,
		TranscriptHash: hash, PromptVersion: PromptVersion,
	}); err != nil {
		return err
	}
	if err := q.PromoteContextJobs(ctx, &db.PromoteContextJobsParams{VideoID: videoID, TranscriptHash: hash}); err != nil {
		return err
	}
	return q.SupersedeStaleContextJobs(ctx, &db.SupersedeStaleContextJobsParams{VideoID: videoID, TranscriptHash: hash})
}

// Job returns the current context-window ML job for a video transcript fingerprint.
func Job(ctx context.Context, dbc *db.DatabaseConnection, videoID pgtype.UUID) (*db.MlJob, error) {
	hash, err := dbc.Queries(ctx).GetTranscriptFingerprint(ctx, videoID)
	if err != nil {
		return nil, err
	}
	return dbc.Queries(ctx).GetMLJobByKey(ctx, &db.GetMLJobByKeyParams{
		VideoID: videoID, Kind: "context_windows", TranscriptHash: hash,
	})
}
