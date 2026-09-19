package contextwindow

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
)

// EnqueueRetry creates a fresh generation identity so completed sets and chunk
// checkpoints cannot swallow an explicit request to try again.
func EnqueueRetry(ctx context.Context, q *db.Queries, videoID pgtype.UUID, instructions string) (*db.MlJob, error) {
	instructions = strings.TrimSpace(instructions)
	if len(instructions) > 4000 {
		return nil, fmt.Errorf("guidance must be at most 4000 bytes")
	}
	t, err := q.GetVideoTranscript(ctx, videoID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(t.Text) == "" {
		return nil, fmt.Errorf("generate a transcript first")
	}
	hash, err := q.GetTranscriptFingerprint(ctx, videoID)
	if err != nil {
		return nil, err
	}
	return q.EnqueueGenerationRetry(ctx, &db.EnqueueGenerationRetryParams{
		VideoID: videoID, Kind: "context_windows", TranscriptHash: hash,
		PromptVersion: PromptVersion + ":retry:" + uuid.NewString(), RetryInstructions: instructions,
	})
}

// GenerationVersion retains the fresh cache identity of an explicit retry.
func GenerationVersion(version string) string {
	if strings.HasPrefix(version, PromptVersion+":retry:") {
		return version
	}
	return PromptVersion
}
