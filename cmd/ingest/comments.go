package main

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/comments"
	"thirdcoast.systems/rewind/internal/db"
)

// ingestCommentsFromInfoJSON delegates to the shared comments ingester (used by
// both the initial ingest and the downloader's comment catch-up loop).
func ingestCommentsFromInfoJSON(ctx context.Context, q *db.Queries, videoID pgtype.UUID, source string, rawInfoJSON []byte) error {
	return comments.IngestFromInfoJSON(ctx, q, videoID, source, rawInfoJSON)
}
