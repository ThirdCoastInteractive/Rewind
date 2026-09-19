package comments

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
)

// LinkIdentitiesForVideo upserts commenters for a video, attaches commenter_id,
// refreshes stats/names, and links archived channels when possible.
func LinkIdentitiesForVideo(ctx context.Context, q Store, videoID pgtype.UUID) error {
	if q == nil {
		return fmt.Errorf("link identities: nil querier")
	}
	if err := q.UpsertCommentersForVideo(ctx, videoID); err != nil {
		return fmt.Errorf("upsert commenters: %w", err)
	}
	if _, err := q.AttachCommentersForVideo(ctx, videoID); err != nil {
		return fmt.Errorf("attach commenters: %w", err)
	}
	if err := q.RefreshCommenterStatsForVideo(ctx, videoID); err != nil {
		return fmt.Errorf("refresh commenter stats: %w", err)
	}
	if err := q.UpsertCommenterNamesForVideo(ctx, videoID); err != nil {
		return fmt.Errorf("upsert commenter names: %w", err)
	}
	if err := q.LinkCommentersToChannelsForVideo(ctx, videoID); err != nil {
		return fmt.Errorf("link commenters to channels: %w", err)
	}
	return nil
}
