package main

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/comments"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/videoid"
	"thirdcoast.systems/rewind/pkg/encryption"
	"thirdcoast.systems/rewind/pkg/ytdlp"
)

const (
	commentCatchupInterval = 5 * time.Minute
	commentCatchupBatch    = 3
	commentCatchupThrottle = 3 * time.Second
)

// commentCatchupLoop periodically backfills comments for older videos that have
// none (e.g. downloaded before comment ingest existed), re-fetching them via
// yt-dlp. It is deliberately gentle (small batches, throttled, newest-first) to
// respect source rate limits.
func commentCatchupLoop(ctx context.Context, dbc *db.DatabaseConnection, encMgr *encryption.Manager) {
	// Let startup settle before the first run.
	select {
	case <-ctx.Done():
		return
	case <-time.After(30 * time.Second):
	}
	runCommentCatchupUnit(ctx, dbc, encMgr)

	ticker := time.NewTicker(commentCatchupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runCommentCatchupUnit(ctx, dbc, encMgr)
		}
	}
}

func runCommentCatchupUnit(ctx context.Context, dbc *db.DatabaseConnection, encMgr *encryption.Manager) {
	q := dbc.Queries(ctx)
	// Atomically claim + mark a small batch (other replicas skip these).
	rows, err := q.ClaimVideosForCommentCatchup(ctx, commentCatchupBatch)
	if err != nil {
		slog.Warn("comment catchup claim failed", "error", err)
		return
	}
	if len(rows) == 0 {
		return
	}
	slog.Info("comment catchup unit", "videos", len(rows))
	for _, r := range rows {
		if ctx.Err() != nil {
			return
		}
		fetchAndIngestComments(ctx, q, encMgr, r.ID, r.Src, r.ArchivedBy)
		select {
		case <-ctx.Done():
			return
		case <-time.After(commentCatchupThrottle):
		}
	}
}

// fetchAndIngestComments re-fetches one video's comments via yt-dlp and ingests
// them. Best-effort: failures are logged, not retried here (the claim already
// marked the video checked, so it won't be re-picked for 30 days).
func fetchAndIngestComments(ctx context.Context, q *db.Queries, encMgr *encryption.Manager, videoID pgtype.UUID, src string, archivedBy pgtype.UUID) {
	src = strings.TrimSpace(src)
	if src == "" {
		return
	}

	client := ytdlp.New()
	client.Path = "/usr/local/bin/yt-dlp"
	// Best-effort cookies (needed for age-restricted/private; public works without).
	if cookies, err := q.GetUserCookies(ctx, archivedBy); err == nil && len(cookies) > 0 {
		if content := generateCookiesFile(encMgr, cookies); strings.TrimSpace(content) != "" {
			client.Cookies = content
		}
	}

	fetchCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	info, err := client.GetInfo(fetchCtx, src,
		"--write-comments",
		"--extractor-args", "youtube:max_comments=4000,all,all,8",
	)
	if err != nil {
		slog.Warn("comment catchup fetch failed", "video_id", uuidString(videoID), "error", err)
		return
	}

	// source = canonical domain, matching the initial ingest path so the
	// (video_id, source, comment_id) unique key aligns.
	source := "unknown"
	if _, canon, derr := videoid.NormalizeSourceURL(src); derr == nil && strings.TrimSpace(canon) != "" {
		source = canon
	}

	if err := comments.IngestFromInfoJSON(ctx, q, videoID, source, info.Raw); err != nil {
		slog.Warn("comment catchup ingest failed", "video_id", uuidString(videoID), "error", err)
	}
}
