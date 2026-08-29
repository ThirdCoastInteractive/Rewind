package main

import (
	"context"
	"log/slog"

	"thirdcoast.systems/rewind/internal/channelid"
	"thirdcoast.systems/rewind/internal/channellinks"
	"thirdcoast.systems/rewind/internal/creatorlink"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/mediaformat"
	"thirdcoast.systems/rewind/pkg/videoinfo"
)

func attachChannel(ctx context.Context, q *db.Queries, video *db.Video, src string, info videoinfo.VideoInfo) {
	if video == nil {
		return
	}
	id := channelid.FromMetadata(src, video.Uploader, video.ChannelID, video.UploaderID, derefStr(video.ChannelURL), derefStr(video.UploaderURL))
	ch, err := q.UpsertChannel(ctx, &db.UpsertChannelParams{
		Platform:     id.Platform,
		IdentityKey:  id.Key,
		ChannelID:    id.ChannelID,
		Uploader:     id.Uploader,
		CanonicalURL: id.CanonicalURL,
	})
	if err != nil {
		slog.Warn("upsert channel failed", "video_id", video.ID, "error", err)
		return
	}
	dur := 0
	if video.DurationSeconds != nil {
		dur = int(*video.DurationSeconds)
	}
	format := mediaformat.Classify(src, dur, info.WasLive, info.LiveStatus)
	if err := q.SetVideoChannelAndFormat(ctx, &db.SetVideoChannelAndFormatParams{
		ID:           video.ID,
		ChannelRowID: ch.ID,
		Format:       format,
	}); err != nil {
		slog.Warn("set video channel/format failed", "video_id", video.ID, "error", err)
	}
	if err := channellinks.HarvestVideo(ctx, q, ch.ID, video.ID, video.Title, video.Description); err != nil {
		slog.Warn("harvest channel links failed", "video_id", video.ID, "error", err)
	} else if err := q.MarkVideoLinksHarvested(ctx, video.ID); err != nil {
		slog.Warn("mark links harvested failed", "video_id", video.ID, "error", err)
	}
	if err := q.ResolveChannelEdges(ctx); err != nil {
		slog.Warn("resolve channel edges failed", "video_id", video.ID, "error", err)
	}
}

func runLinkHarvestUnit(ctx context.Context, dbc *db.DatabaseConnection) {
	select {
	case <-ctx.Done():
		return
	default:
	}
	q := dbc.Queries(ctx)
	if err := q.RelabelTwitterMentionsMisfiledAsYouTube(ctx); err != nil {
		slog.Warn("relabel twitter mentions failed", "error", err)
	}
	rows, err := q.ClaimVideosForLinkHarvest(ctx, 40)
	if err != nil {
		slog.Warn("link harvest claim failed", "error", err)
		return
	}
	if len(rows) == 0 {
		return
	}
	slog.Info("link harvest unit", "videos", len(rows))
	for _, row := range rows {
		if row == nil || !row.ChannelRowID.Valid {
			continue
		}
		if err := channellinks.HarvestVideo(ctx, q, row.ChannelRowID, row.ID, row.Title, row.Description); err != nil {
			slog.Warn("link harvest failed", "video_id", row.ID, "error", err)
		}
	}
	if err := q.ResolveChannelEdges(ctx); err != nil {
		slog.Warn("resolve channel edges failed", "error", err)
	}
	creatorlink.Apply(ctx, q)
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
