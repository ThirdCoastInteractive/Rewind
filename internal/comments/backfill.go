package comments

import (
	"context"
	"fmt"
	"log/slog"

	"thirdcoast.systems/rewind/internal/db"
)

// RunIdentityBackfill links commenter identities for a small batch of videos
// that still have unlinked comments. Does not fetch yt-dlp; only links rows.
func RunIdentityBackfill(ctx context.Context, dbc *db.DatabaseConnection, batchSize int32) (int, error) {
	if dbc == nil {
		return 0, fmt.Errorf("identity backfill: nil database")
	}
	if batchSize <= 0 {
		batchSize = 5
	}
	q := dbc.Queries(ctx)
	ids, err := q.ListVideosNeedingCommenters(ctx, batchSize)
	if err != nil {
		return 0, fmt.Errorf("list videos needing commenters: %w", err)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	n := 0
	for _, videoID := range ids {
		if ctx.Err() != nil {
			return n, ctx.Err()
		}
		if err := LinkIdentitiesForVideo(ctx, q, videoID); err != nil {
			slog.Warn("commenter identity backfill failed", "video_id", videoID, "error", err)
			continue
		}
		n++
	}
	return n, nil
}
