package textcls

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/pkg/plugin"
)

// EnqueueCommentClassifyJob queues kind=comment_classify off the HTTP path.
func EnqueueCommentClassifyJob(ctx context.Context, videoID pgtype.UUID, transcriptHash, modelDigest string) error {
	return plugin.Enqueue(ctx, plugin.Job{
		VideoID:        uuid.UUID(videoID.Bytes).String(),
		Kind:           plugin.KindClassify,
		Priority:       Priority,
		TranscriptHash: transcriptHash,
		ModelDigest:    modelDigest,
		PromptVersion:  PromptVersion,
	})
}
