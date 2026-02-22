package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
)

// ingestCommentsFromInfoJSON extracts the "comments" array from a raw info.json
// byte slice and upserts them into the normalized video_comments table.
// It is a best-effort operation: missing or empty comments are silently skipped.
func ingestCommentsFromInfoJSON(ctx context.Context, q *db.Queries, videoID pgtype.UUID, source string, rawInfoJSON []byte) error {
	// Quick check: does the JSON even contain a comments key?
	var envelope struct {
		Comments json.RawMessage `json:"comments"`
	}
	if err := json.Unmarshal(rawInfoJSON, &envelope); err != nil {
		return nil // not valid JSON - skip silently
	}
	if len(envelope.Comments) == 0 || string(envelope.Comments) == "null" {
		return nil // no comments embedded
	}

	// Validate that comments is actually an array
	var arr []json.RawMessage
	if err := json.Unmarshal(envelope.Comments, &arr); err != nil {
		return nil // not an array - skip
	}
	if len(arr) == 0 {
		return nil
	}

	slog.Info("ingesting comments from info.json", "video_id", videoID, "source", source, "count", len(arr))

	// Batch in chunks of 500 to avoid massive single queries
	const batchSize = 500
	for i := 0; i < len(arr); i += batchSize {
		end := i + batchSize
		if end > len(arr) {
			end = len(arr)
		}
		chunk := arr[i:end]

		chunkJSON, err := json.Marshal(chunk)
		if err != nil {
			return fmt.Errorf("marshal comment chunk: %w", err)
		}

		if err := q.UpsertVideoCommentsFromJSON(ctx, &db.UpsertVideoCommentsFromJSONParams{
			VideoID:      videoID,
			Source:       source,
			CommentsJson: chunkJSON,
		}); err != nil {
			return fmt.Errorf("upsert comments batch %d-%d: %w", i, end, err)
		}
	}

	slog.Info("comments ingested successfully", "video_id", videoID, "total", len(arr))
	return nil
}
