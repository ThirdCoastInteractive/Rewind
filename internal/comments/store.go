package comments

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
)

// Store is the comment ingest surface. *db.Queries implements it.
type Store interface {
	UpsertVideoCommentsFromJSON(ctx context.Context, arg *db.UpsertVideoCommentsFromJSONParams) error
	RefreshVideoCommentCount(ctx context.Context, videoID pgtype.UUID) error
	UpsertCommentersForVideo(ctx context.Context, videoID pgtype.UUID) error
	AttachCommentersForVideo(ctx context.Context, videoID pgtype.UUID) (int64, error)
	RefreshCommenterStatsForVideo(ctx context.Context, videoID pgtype.UUID) error
	UpsertCommenterNamesForVideo(ctx context.Context, videoID pgtype.UUID) error
	LinkCommentersToChannelsForVideo(ctx context.Context, videoID pgtype.UUID) error
	GetVideoByID(ctx context.Context, id pgtype.UUID) (*db.Video, error)
	CommentClassifyInputHash(ctx context.Context, videoID pgtype.UUID) (string, error)
}
