package download

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"thirdcoast.systems/rewind/internal/automarkers"
	"thirdcoast.systems/rewind/internal/channellinks"
	"thirdcoast.systems/rewind/internal/comments"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/videoid"
	"thirdcoast.systems/rewind/pkg/encryption"
	"thirdcoast.systems/rewind/pkg/videoinfo"
	"thirdcoast.systems/rewind/pkg/ytdlp"
)

const (
	metadataRefreshInterval = 5 * time.Minute
	metadataRefreshBatch    = 3
	metadataRefreshThrottle = 3 * time.Second
)

// metadataRefreshLoop periodically re-fetches title/description/tags/view counts
// and comments for archived videos via yt-dlp --skip-download. It is deliberately
// gentle (small batches, throttled) to respect source rate limits. Followed-
// channel videos are preferred by the claim query's ORDER BY.
func metadataRefreshLoop(ctx context.Context, dbc *db.DatabaseConnection, encMgr *encryption.Manager, ytdlpPath string) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(30 * time.Second):
	}
	runMetadataRefreshUnit(ctx, dbc, encMgr, ytdlpPath)

	ticker := time.NewTicker(metadataRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runMetadataRefreshUnit(ctx, dbc, encMgr, ytdlpPath)
		}
	}
}

func runMetadataRefreshUnit(ctx context.Context, dbc *db.DatabaseConnection, encMgr *encryption.Manager, ytdlpPath string) {
	q := dbc.Queries(ctx)
	rows, err := q.ClaimVideosForMetadataRefresh(ctx, metadataRefreshBatch)
	if err != nil {
		slog.Warn("metadata refresh claim failed", "error", err)
		return
	}
	if len(rows) == 0 {
		return
	}
	slog.Info("metadata refresh unit", "videos", len(rows))
	for _, r := range rows {
		if ctx.Err() != nil {
			return
		}
		refreshVideoMetadata(ctx, q, encMgr, ytdlpPath, r)
		select {
		case <-ctx.Done():
			return
		case <-time.After(metadataRefreshThrottle):
		}
	}
	if err := channellinks.ResolvePendingEdges(ctx, q); err != nil {
		slog.Warn("metadata refresh resolve channel edges", "error", err)
	}
}

// refreshVideoMetadata re-fetches one video's info.json (skip-download) and
// updates metadata + comments. Best-effort: failures are logged, not retried
// here (the claim already marked the video refreshed, so it won't be re-picked
// for 7 days).
func refreshVideoMetadata(ctx context.Context, q *db.Queries, encMgr *encryption.Manager, ytdlpPath string, row *db.ClaimVideosForMetadataRefreshRow) {
	src := strings.TrimSpace(row.Src)
	if src == "" {
		return
	}

	client := ytdlp.New()
	client.Path = ytdlpPath
	if cookies, err := q.GetUserCookies(ctx, row.ArchivedBy); err == nil && len(cookies) > 0 {
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
		slog.Warn("metadata refresh fetch failed", "video_id", uuidString(row.ID), "error", err)
		return
	}

	parsed, ok := parseRefreshMetadata(info.Raw, info.Title)
	if !ok {
		slog.Warn("metadata refresh parse failed", "video_id", uuidString(row.ID))
		return
	}

	title := strings.TrimSpace(parsed.Title)
	if title == "" {
		title = strings.TrimSpace(row.Title)
	}
	uploader := strings.TrimSpace(parsed.Uploader)
	if uploader == "" {
		uploader = row.Uploader
	}
	description := parsed.Description
	if description == "" {
		description = row.Description
	}
	tags := parsed.Tags
	if len(tags) == 0 && len(row.Tags) > 0 {
		tags = row.Tags
	}
	if tags == nil {
		tags = []string{}
	}

	viewCount, likeCount := parsed.ViewCount, parsed.LikeCount
	if viewCount == nil || likeCount == nil {
		if existing, gerr := q.GetVideoByID(ctx, row.ID); gerr == nil && existing != nil {
			if viewCount == nil {
				viewCount = existing.ViewCount
			}
			if likeCount == nil {
				likeCount = existing.LikeCount
			}
		}
	}

	infoVI, err := videoinfo.NewVideoInfo(info.Raw)
	if err != nil {
		slog.Warn("metadata refresh info json failed", "video_id", uuidString(row.ID), "error", err)
	}

	if err := q.RefreshVideoMetadata(ctx, &db.RefreshVideoMetadataParams{
		Title:       title,
		Description: description,
		Tags:        tags,
		ViewCount:   viewCount,
		LikeCount:   likeCount,
		Info:        infoVI,
		Uploader:    uploader,
		ID:          row.ID,
	}); err != nil {
		slog.Warn("metadata refresh update failed", "video_id", uuidString(row.ID), "error", err)
	}

	source := "unknown"
	if _, canon, derr := videoid.NormalizeSourceURL(src); derr == nil && strings.TrimSpace(canon) != "" {
		source = canon
	}

	if err := comments.IngestFromInfoJSON(ctx, q, row.ID, source, info.Raw); err != nil {
		slog.Warn("metadata refresh comment ingest failed", "video_id", uuidString(row.ID), "error", err)
	}

	automarkers.IngestFromInfoJSON(ctx, q, &db.Video{
		ID:              row.ID,
		Description:     description,
		DurationSeconds: row.DurationSeconds,
	}, info.Raw)

	if row.ChannelRowID.Valid {
		if err := channellinks.HarvestVideo(ctx, q, row.ChannelRowID, row.ID, title, description); err != nil {
			slog.Warn("metadata refresh link harvest failed", "video_id", uuidString(row.ID), "error", err)
		}
	}
}

// refreshMetadata is the subset of yt-dlp info.json fields written back on a
// skip-download refresh. Shapes match cmd/ingest/normalize.go.
type refreshMetadata struct {
	Title       string
	Description string
	Tags        []string
	Uploader    string
	ViewCount   *int64
	LikeCount   *int64
}

func parseRefreshMetadata(raw []byte, infoTitle string) (refreshMetadata, bool) {
	out := refreshMetadata{Tags: []string{}, Title: strings.TrimSpace(infoTitle)}

	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return out, false
	}

	if v, ok := m["title"].(string); ok {
		if t := strings.TrimSpace(v); t != "" {
			out.Title = t
		}
	}
	if v, ok := m["description"].(string); ok {
		out.Description = strings.TrimSpace(v)
	}
	if v, ok := m["uploader"].(string); ok {
		out.Uploader = strings.TrimSpace(v)
	}

	if arr, ok := m["tags"].([]any); ok {
		out.Tags = out.Tags[:0]
		for _, it := range arr {
			s, ok := it.(string)
			if !ok {
				continue
			}
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			out.Tags = append(out.Tags, s)
		}
	}

	if v, ok := m["view_count"].(float64); ok {
		c := int64(v)
		out.ViewCount = &c
	}
	if v, ok := m["like_count"].(float64); ok {
		c := int64(v)
		out.LikeCount = &c
	}

	return out, true
}
