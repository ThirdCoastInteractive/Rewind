// Package comments ingests yt-dlp comment data (from an info.json blob) into the
// normalized video_comments table. Shared by the ingest service (initial
// download) and the downloader's comment catch-up loop (backfill).
package comments

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/automarkers"
	"thirdcoast.systems/rewind/internal/channellinks"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/textcls"
)

// IngestFromInfoJSON extracts the "comments" array from a raw info.json byte
// slice and upserts them into video_comments. Best-effort: missing or empty
// comments are silently skipped.
func IngestFromInfoJSON(ctx context.Context, q Store, videoID pgtype.UUID, source string, rawInfoJSON []byte) error {
	var envelope struct {
		Comments json.RawMessage `json:"comments"`
	}
	if err := json.Unmarshal(rawInfoJSON, &envelope); err != nil {
		return nil
	}
	if len(envelope.Comments) == 0 || string(envelope.Comments) == "null" {
		return nil
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(envelope.Comments, &arr); err != nil {
		return nil
	}
	if len(arr) == 0 {
		return nil
	}
	if err := ingestCommentJSONArray(ctx, q, videoID, source, arr); err != nil {
		return err
	}
	if queries, ok := q.(*db.Queries); ok {
		if video, err := queries.GetVideoByID(ctx, videoID); err == nil && video != nil {
			automarkers.IngestFromInfoJSON(ctx, queries, video, rawInfoJSON)
			if envelope.Comments != nil {
				automarkers.IngestCommentJSON(ctx, queries, video, envelope.Comments)
			}
			if video.ChannelRowID.Valid {
				if err := channellinks.HarvestComments(ctx, queries, video.ChannelRowID, video.ID); err != nil {
					slog.Warn("harvest comment channel links failed", "video_id", videoID, "error", err)
				}
			}
		}
	}
	return nil
}

// IngestLiveChatJSON converts a yt-dlp live_chat.json blob into comments with
// source youtube.com/live_chat and the same identity path as video comments.
func IngestLiveChatJSON(ctx context.Context, q Store, videoID pgtype.UUID, raw []byte) error {
	rows := LiveChatToComments(raw)
	if len(rows) == 0 {
		return nil
	}
	arr := make([]json.RawMessage, 0, len(rows))
	for _, row := range rows {
		b, err := json.Marshal(row)
		if err != nil {
			return fmt.Errorf("marshal live chat comment: %w", err)
		}
		arr = append(arr, b)
	}
	return ingestCommentJSONArray(ctx, q, videoID, LiveChatSource, arr)
}

func ingestCommentJSONArray(ctx context.Context, q Store, videoID pgtype.UUID, source string, arr []json.RawMessage) error {
	if q == nil {
		return fmt.Errorf("ingest comments: nil store")
	}
	slog.Info("ingesting comments", "video_id", videoID, "source", source, "count", len(arr))
	const batchSize = 500
	for i := 0; i < len(arr); i += batchSize {
		end := i + batchSize
		if end > len(arr) {
			end = len(arr)
		}
		chunkJSON, err := json.Marshal(arr[i:end])
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
	if err := q.RefreshVideoCommentCount(ctx, videoID); err != nil {
		return fmt.Errorf("refresh cached comment count: %w", err)
	}
	if err := LinkIdentitiesForVideo(ctx, q, videoID); err != nil {
		return fmt.Errorf("link commenter identities: %w", err)
	}
	return enqueueCommentClassify(ctx, q, videoID)
}

func enqueueCommentClassify(ctx context.Context, q Store, videoID pgtype.UUID) error {
	hash, err := q.CommentClassifyInputHash(ctx, videoID)
	if err != nil {
		slog.Warn("comment classify hash failed", "video_id", videoID, "error", err)
		return nil
	}
	return textcls.EnqueueCommentClassifyJob(ctx, videoID, hash, "")
}
