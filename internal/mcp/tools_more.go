package mcp

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"thirdcoast.systems/rewind/internal/analyze"
	"thirdcoast.systems/rewind/internal/archival"
	"thirdcoast.systems/rewind/internal/channelid"
	"thirdcoast.systems/rewind/internal/db"
)

type filterArgs struct {
	Filter string `json:"filter,omitempty" jsonschema:"Optional name substring"`
}

type urlArgs struct {
	URL   string `json:"url" jsonschema:"Channel, playlist, or video URL"`
	Label string `json:"label,omitempty" jsonschema:"Optional watch label"`
}

type unfollowArgs struct {
	URL string `json:"url,omitempty" jsonschema:"Watched channel URL"`
	ID  string `json:"id,omitempty" jsonschema:"Watch UUID"`
}

type creatorNameArgs struct {
	Name  string `json:"name" jsonschema:"Creator display name"`
	Notes string `json:"notes,omitempty"`
}

type linkCreatorArgs struct {
	ChannelID string `json:"channel_id" jsonschema:"Channel row UUID"`
	CreatorID string `json:"creator_id" jsonschema:"Creator UUID"`
}

type tagArgs struct {
	VideoID string `json:"video_id" jsonschema:"Video UUID"`
	Tag     string `json:"tag" jsonschema:"Tag name"`
}

type commentSearchArgs struct {
	Query   string `json:"query" jsonschema:"Search text"`
	VideoID string `json:"video_id" jsonschema:"Video UUID"`
	Limit   int32  `json:"limit,omitempty" jsonschema:"Max results, default 20"`
}

func getRelated(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *idArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *idArgs) (*mcpsdk.CallToolResult, any, error) {
		id, err := parseUUID(args.ID)
		if err != nil {
			return nil, nil, err
		}
		q := dbc.Queries(ctx)
		v, err := q.GetVideoByID(ctx, id)
		if err != nil || v == nil {
			return nil, nil, fmt.Errorf("video not found")
		}
		out := map[string]any{
			"video": videoCitation(v.ID, v.Title, v.Uploader),
		}
		if up := strings.TrimSpace(v.Uploader); up != "" {
			rows, err := q.ListVideosPaginated(ctx, &db.ListVideosPaginatedParams{
				Uploader:   &up,
				SortOrder:  "published-newest",
				PageOffset: 0,
				PageLimit:  10,
			})
			if err != nil {
				return nil, nil, err
			}
			related := make([]citation, 0, len(rows))
			self := uuidString(v.ID)
			for _, r := range rows {
				if uuidString(r.ID) == self {
					continue
				}
				related = append(related, videoCitation(r.ID, r.Title, r.Uploader))
			}
			out["same_channel"] = related
		}
		clips, err := q.ListClipsByVideo(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		out["clips"] = clipCitations(clips)
		markers, err := q.ListMarkersByVideo(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		out["markers"] = markerCitations(markers)
		if v.ChannelRowID.Valid {
			edges, err := q.ListChannelEdgesForChannel(ctx, v.ChannelRowID)
			if err != nil {
				return nil, nil, err
			}
			out["neighbor_channels"] = edges
		}
		return jsonResult(out)
	}
}

func getChannelGraph(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *struct{}) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ *struct{}) (*mcpsdk.CallToolResult, any, error) {
		q := dbc.Queries(ctx)
		edges, err := q.ListChannelEdges(ctx)
		if err != nil {
			return nil, nil, err
		}
		seen := map[string]struct{}{}
		nodes := make([]map[string]any, 0)
		addNode := func(id pgtype.UUID) {
			sid := uuidString(id)
			if sid == "" {
				return
			}
			if _, ok := seen[sid]; ok {
				return
			}
			seen[sid] = struct{}{}
			node := map[string]any{
				"kind": "channel",
				"id":   sid,
				"uri":  "rewind://channel/" + sid,
			}
			if ch, err := q.GetChannel(ctx, id); err == nil && ch != nil {
				node["title"] = ch.Uploader
				node["uploader"] = ch.Uploader
				node["platform"] = ch.Platform
				node["web_path"] = channelWebPath(ch.Uploader)
				if ch.CanonicalURL != "" {
					node["canonical_url"] = ch.CanonicalURL
				}
			}
			nodes = append(nodes, node)
		}
		for _, e := range edges {
			addNode(e.FromChannelID)
			addNode(e.ToChannelID)
		}
		return jsonResult(map[string]any{
			"nodes": nodes,
			"edges": edges,
		})
	}
}

func listChannels(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *filterArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *filterArgs) (*mcpsdk.CallToolResult, any, error) {
		rows, err := dbc.Queries(ctx).ListChannels(ctx, nullable(args.Filter))
		if err != nil {
			return nil, nil, err
		}
		out := make([]citation, 0, len(rows))
		for _, r := range rows {
			out = append(out, citation{
				Kind:     "channel",
				Title:    r.Uploader,
				Uploader: r.Uploader,
				URI:      "rewind://channel/" + url.PathEscape(r.Uploader),
				WebPath:  channelWebPath(r.Uploader),
			})
		}
		return jsonResult(out)
	}
}

func getChannel(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *idArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *idArgs) (*mcpsdk.CallToolResult, any, error) {
		q := dbc.Queries(ctx)
		raw := strings.TrimSpace(args.ID)
		if id, err := parseUUID(raw); err == nil {
			ch, err := q.GetChannel(ctx, id)
			if err == nil && ch != nil {
				return jsonResult(channelRowJSON(ch))
			}
		}
		overview, err := q.GetChannelOverview(ctx, raw)
		if err != nil || overview == nil {
			return nil, nil, fmt.Errorf("channel not found")
		}
		return jsonResult(map[string]any{
			"kind":                   "channel",
			"title":                  overview.Uploader,
			"uploader":               overview.Uploader,
			"uri":                    "rewind://channel/" + url.PathEscape(overview.Uploader),
			"web_path":               channelWebPath(overview.Uploader),
			"video_count":            overview.VideoCount,
			"total_duration_seconds": overview.TotalDurationSeconds,
			"total_size_bytes":       overview.TotalSizeBytes,
			"total_views":            overview.TotalViews,
			"channel_url":            overview.ChannelURL,
			"uploader_url":           overview.UploaderURL,
			"watch_id":               uuidString(overview.WatchID),
		})
	}
}

func listCreators(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *filterArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *filterArgs) (*mcpsdk.CallToolResult, any, error) {
		rows, err := dbc.Queries(ctx).ListCreators(ctx)
		if err != nil {
			return nil, nil, err
		}
		out := make([]map[string]any, 0, len(rows))
		for _, r := range rows {
			out = append(out, creatorListJSON(r))
		}
		return jsonResult(out)
	}
}

func getCreator(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *idArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *idArgs) (*mcpsdk.CallToolResult, any, error) {
		id, err := parseUUID(args.ID)
		if err != nil {
			return nil, nil, err
		}
		q := dbc.Queries(ctx)
		cr, err := q.GetCreator(ctx, id)
		if err != nil || cr == nil {
			return nil, nil, fmt.Errorf("creator not found")
		}
		chans, err := q.ListChannelsByCreator(ctx, cr.ID)
		if err != nil {
			return nil, nil, err
		}
		channels := make([]map[string]any, 0, len(chans))
		for _, ch := range chans {
			channels = append(channels, channelRowJSON(ch))
		}
		out := creatorJSON(cr)
		out["channels"] = channels
		return jsonResult(out)
	}
}

func analyzeCreator(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *idArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *idArgs) (*mcpsdk.CallToolResult, any, error) {
		id, err := parseUUID(args.ID)
		if err != nil {
			return nil, nil, err
		}
		q := dbc.Queries(ctx)
		cr, err := q.GetCreator(ctx, id)
		if err != nil || cr == nil {
			return nil, nil, fmt.Errorf("creator not found")
		}
		chans, err := q.ListChannelsByCreator(ctx, cr.ID)
		if err != nil {
			return nil, nil, err
		}
		ids := make([]pgtype.UUID, 0, len(chans))
		for _, ch := range chans {
			ids = append(ids, ch.ID)
		}
		rows, err := q.ListVideosForAnalyze(ctx, ids)
		if err != nil {
			return nil, nil, err
		}
		videos := make([]analyze.Video, 0, len(rows))
		for _, r := range rows {
			videos = append(videos, analyzeVideo(r))
		}
		if len(videos) == 0 {
			for _, ch := range chans {
				up := strings.TrimSpace(ch.Uploader)
				if up == "" {
					continue
				}
				page, err := q.ListVideosPaginated(ctx, &db.ListVideosPaginatedParams{
					Uploader:   &up,
					SortOrder:  "published-newest",
					PageOffset: 0,
					PageLimit:  200,
				})
				if err != nil {
					return nil, nil, err
				}
				for _, r := range page {
					videos = append(videos, analyzeVideoFromPage(r, ch.Platform))
				}
			}
		}
		report := analyze.Analyze(videos, time.Now())
		channelOut := make([]map[string]any, 0, len(chans))
		for _, ch := range chans {
			channelOut = append(channelOut, channelRowJSON(ch))
		}
		return jsonResult(map[string]any{
			"creator":           creatorJSON(cr),
			"channels":          channelOut,
			"video_count":       len(videos),
			"status":            report.Status,
			"signals":           report.Signals,
			"stats":             report.Stats,
			"old_format_mix":    report.OldFormatMix,
			"recent_format_mix": report.RecentFormatMix,
		})
	}
}

func suggestChannelLinks(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *struct{}) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ *struct{}) (*mcpsdk.CallToolResult, any, error) {
		rows, err := dbc.Queries(ctx).ListUnassignedChannels(ctx)
		if err != nil {
			return nil, nil, err
		}
		type pair struct {
			A      map[string]any `json:"a"`
			B      map[string]any `json:"b"`
			Reason string         `json:"reason"`
		}
		out := make([]pair, 0)
		for i := 0; i < len(rows); i++ {
			for j := i + 1; j < len(rows); j++ {
				reason, ok := similarUploaderReason(rows[i].Uploader, rows[j].Uploader)
				if !ok {
					continue
				}
				out = append(out, pair{
					A:      channelRowJSON(rows[i]),
					B:      channelRowJSON(rows[j]),
					Reason: reason,
				})
			}
		}
		return jsonResult(out)
	}
}

func searchComments(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *commentSearchArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *commentSearchArgs) (*mcpsdk.CallToolResult, any, error) {
		vid, err := parseUUID(args.VideoID)
		if err != nil {
			return nil, nil, err
		}
		if strings.TrimSpace(args.Query) == "" {
			return jsonResult([]citation{})
		}
		if args.Limit <= 0 || args.Limit > 50 {
			args.Limit = 20
		}
		rows, err := dbc.Queries(ctx).SearchVideoComments(ctx, &db.SearchVideoCommentsParams{
			Query:      args.Query,
			VideoID:    vid,
			PageOffset: 0,
			PageSize:   args.Limit,
		})
		if err != nil {
			return nil, nil, err
		}
		sid := uuidString(vid)
		out := make([]citation, 0, len(rows))
		for _, r := range rows {
			quote := ""
			if r.Text != nil {
				quote = *r.Text
			}
			title := ""
			if r.Author != nil {
				title = *r.Author
			}
			out = append(out, citation{
				Kind:    "comment",
				ID:      uuidString(r.ID),
				URI:     "rewind://video/" + sid,
				WebPath: "/videos/" + sid,
				Title:   title,
				Quote:   quote,
				VideoID: sid,
			})
		}
		return jsonResult(out)
	}
}

func listClips(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *idArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *idArgs) (*mcpsdk.CallToolResult, any, error) {
		id, err := parseUUID(args.ID)
		if err != nil {
			return nil, nil, err
		}
		clips, err := dbc.Queries(ctx).ListClipsByVideo(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(clipCitations(clips))
	}
}

func listMarkers(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *idArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *idArgs) (*mcpsdk.CallToolResult, any, error) {
		id, err := parseUUID(args.ID)
		if err != nil {
			return nil, nil, err
		}
		markers, err := dbc.Queries(ctx).ListMarkersByVideo(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(markerCitations(markers))
	}
}

func listFollows(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *struct{}) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ *struct{}) (*mcpsdk.CallToolResult, any, error) {
		rows, err := dbc.Queries(ctx).ListWatchedChannels(ctx)
		if err != nil {
			return nil, nil, err
		}
		out := make([]map[string]any, 0, len(rows))
		for _, r := range rows {
			item := map[string]any{
				"kind":             "follow",
				"id":               uuidString(r.ID),
				"url":              r.URL,
				"label":            r.Label,
				"enabled":          r.Enabled,
				"cron_schedule":    r.CronSchedule,
				"backfill":         r.Backfill,
				"tracked_count":    r.TrackedCount,
				"last_scan_status": r.LastScanStatus,
				"last_scan_found":  r.LastScanFound,
			}
			if r.ChannelID.Valid {
				item["channel_id"] = uuidString(r.ChannelID)
			}
			out = append(out, item)
		}
		return jsonResult(out)
	}
}

func enqueueDownload(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *urlArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *urlArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		tok := tokenFrom(ctx)
		res, err := archival.EnqueueURL(ctx, dbc.Queries(ctx), args.URL, tok.UserID)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(enqueueJSON(res))
	}
}

func followChannel(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *urlArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *urlArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		tok := tokenFrom(ctx)
		raw := strings.TrimSpace(args.URL)
		if raw == "" {
			return nil, nil, fmt.Errorf("url is required")
		}
		if !strings.Contains(raw, "://") {
			raw = "https://" + raw
		}
		w, err := dbc.Queries(ctx).CreateWatchedChannel(ctx, &db.CreateWatchedChannelParams{
			CreatedBy:    tok.UserID,
			URL:          raw,
			Label:        strings.TrimSpace(args.Label),
			CronSchedule: "@daily",
			Backfill:     false,
			NextScanAt:   pgtype.Timestamptz{Time: time.Now(), Valid: true},
		})
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{
			"kind":          "follow",
			"id":            uuidString(w.ID),
			"url":           w.URL,
			"label":         w.Label,
			"cron_schedule": w.CronSchedule,
			"enabled":       w.Enabled,
		})
	}
}

func unfollowChannel(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *unfollowArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *unfollowArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		q := dbc.Queries(ctx)
		if strings.TrimSpace(args.ID) != "" {
			id, err := parseUUID(args.ID)
			if err != nil {
				return nil, nil, err
			}
			if err := q.DeleteWatchedChannel(ctx, id); err != nil {
				return nil, nil, err
			}
			return jsonResult(map[string]any{"ok": true, "id": uuidString(id)})
		}
		raw := strings.TrimSpace(args.URL)
		if raw == "" {
			return nil, nil, fmt.Errorf("url or id is required")
		}
		rows, err := q.ListWatchedChannels(ctx)
		if err != nil {
			return nil, nil, err
		}
		tok := tokenFrom(ctx)
		deleted := []string{}
		for _, r := range rows {
			if !strings.EqualFold(strings.TrimSpace(r.URL), raw) {
				continue
			}
			if tok != nil && r.CreatedBy.Valid && uuidString(r.CreatedBy) != uuidString(tok.UserID) {
				continue
			}
			if err := q.DeleteWatchedChannel(ctx, r.ID); err != nil {
				return nil, nil, err
			}
			deleted = append(deleted, uuidString(r.ID))
		}
		if len(deleted) == 0 {
			return nil, nil, fmt.Errorf("watch not found")
		}
		return jsonResult(map[string]any{"ok": true, "ids": deleted})
	}
}

func createCreator(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *creatorNameArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *creatorNameArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		name := strings.TrimSpace(args.Name)
		if name == "" {
			return nil, nil, fmt.Errorf("name is required")
		}
		cr, err := dbc.Queries(ctx).CreateCreator(ctx, &db.CreateCreatorParams{
			Name:  name,
			Notes: strings.TrimSpace(args.Notes),
		})
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(creatorJSON(cr))
	}
}

func linkChannelToCreator(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *linkCreatorArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *linkCreatorArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		chID, err := parseUUID(args.ChannelID)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid channel_id")
		}
		crID, err := parseUUID(args.CreatorID)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid creator_id")
		}
		if err := dbc.Queries(ctx).SetChannelCreator(ctx, &db.SetChannelCreatorParams{
			CreatorID: crID,
			ID:        chID,
		}); err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{
			"ok":         true,
			"channel_id": uuidString(chID),
			"creator_id": uuidString(crID),
		})
	}
}

func unlinkChannel(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *idArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *idArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		chID, err := parseUUID(args.ID)
		if err != nil {
			return nil, nil, err
		}
		if err := dbc.Queries(ctx).SetChannelCreator(ctx, &db.SetChannelCreatorParams{
			CreatorID: pgtype.UUID{},
			ID:        chID,
		}); err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{"ok": true, "channel_id": uuidString(chID)})
	}
}

func addTag(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *tagArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *tagArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		tok := tokenFrom(ctx)
		vid, err := parseUUID(args.VideoID)
		if err != nil {
			return nil, nil, err
		}
		name := strings.TrimSpace(args.Tag)
		slug := tagSlug(name)
		if slug == "" {
			return nil, nil, fmt.Errorf("tag is required")
		}
		q := dbc.Queries(ctx)
		tag, err := q.UpsertTag(ctx, &db.UpsertTagParams{
			Name:      name,
			Slug:      slug,
			CreatedBy: tok.UserID,
		})
		if err != nil {
			return nil, nil, err
		}
		if err := q.AddVideoTag(ctx, &db.AddVideoTagParams{
			VideoID:   vid,
			TagID:     tag.ID,
			CreatedBy: tok.UserID,
		}); err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{
			"ok":       true,
			"video_id": uuidString(vid),
			"tag_id":   uuidString(tag.ID),
			"name":     tag.Name,
			"slug":     tag.Slug,
		})
	}
}

func refreshVideoMetadata(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *idArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *idArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		tok := tokenFrom(ctx)
		id, err := parseUUID(args.ID)
		if err != nil {
			return nil, nil, err
		}
		q := dbc.Queries(ctx)
		v, err := q.GetVideoByID(ctx, id)
		if err != nil || v == nil {
			return nil, nil, fmt.Errorf("video not found")
		}
		if strings.TrimSpace(v.Src) == "" {
			return nil, nil, fmt.Errorf("video has no source URL")
		}
		res, err := archival.EnqueueURL(ctx, q, v.Src, tok.UserID)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(enqueueJSON(res))
	}
}

func findQuotePrompt(_ context.Context, req *mcpsdk.GetPromptRequest) (*mcpsdk.GetPromptResult, error) {
	query := ""
	uploader := ""
	if req != nil && req.Params != nil && req.Params.Arguments != nil {
		query = req.Params.Arguments["query"]
		uploader = req.Params.Arguments["uploader"]
	}
	var b strings.Builder
	b.WriteString("Find this spoken quote in the Rewind archive transcripts")
	if query != "" {
		fmt.Fprintf(&b, ": %q", query)
	}
	b.WriteString(".\n")
	if uploader != "" {
		fmt.Fprintf(&b, "Prefer videos by uploader %q.\n", uploader)
	}
	b.WriteString("Use search_transcripts, then get_transcript if you need surrounding context.\n")
	b.WriteString("Return citation objects with kind, id, uri (rewind://video/{id}), web_path, title, quote, start, and end.")
	return &mcpsdk.GetPromptResult{
		Description: "Find a quote in archived transcripts and cite it.",
		Messages: []*mcpsdk.PromptMessage{{
			Role:    "user",
			Content: &mcpsdk.TextContent{Text: b.String()},
		}},
	}, nil
}

func videoCitation(id pgtype.UUID, title, uploader string) citation {
	sid := uuidString(id)
	return citation{
		Kind:     "video",
		ID:       sid,
		URI:      "rewind://video/" + sid,
		WebPath:  "/videos/" + sid,
		Title:    title,
		Uploader: uploader,
	}
}

func clipCitations(clips []*db.Clip) []citation {
	out := make([]citation, 0, len(clips))
	for _, c := range clips {
		if c == nil {
			continue
		}
		vid := uuidString(c.VideoID)
		st, en := c.StartTs, c.EndTs
		out = append(out, citation{
			Kind:    "clip",
			ID:      uuidString(c.ID),
			URI:     "rewind://video/" + vid,
			WebPath: fmt.Sprintf("/videos/%s?t=%.0f", vid, c.StartTs),
			Title:   c.Title,
			Quote:   c.Description,
			Start:   &st,
			End:     &en,
			VideoID: vid,
		})
	}
	return out
}

func markerCitations(markers []*db.Marker) []citation {
	out := make([]citation, 0, len(markers))
	for _, m := range markers {
		if m == nil {
			continue
		}
		vid := uuidString(m.VideoID)
		st := m.Timestamp
		c := citation{
			Kind:    "marker",
			ID:      uuidString(m.ID),
			URI:     "rewind://video/" + vid,
			WebPath: fmt.Sprintf("/videos/%s?t=%.0f", vid, m.Timestamp),
			Title:   m.Title,
			Quote:   m.Description,
			Start:   &st,
			VideoID: vid,
		}
		if m.Duration != nil {
			en := m.Timestamp + *m.Duration
			c.End = &en
		}
		out = append(out, c)
	}
	return out
}

func channelRowJSON(ch *db.Channel) map[string]any {
	if ch == nil {
		return nil
	}
	id := uuidString(ch.ID)
	out := map[string]any{
		"kind":          "channel",
		"id":            id,
		"uri":           "rewind://channel/" + id,
		"web_path":      channelWebPath(ch.Uploader),
		"title":         ch.Uploader,
		"uploader":      ch.Uploader,
		"platform":      ch.Platform,
		"canonical_url": ch.CanonicalURL,
		"channel_id":    ch.ChannelID,
	}
	if ch.CreatorID.Valid {
		out["creator_id"] = uuidString(ch.CreatorID)
	}
	return out
}

func creatorListJSON(cr *db.ListCreatorsRow) map[string]any {
	if cr == nil {
		return nil
	}
	m := creatorJSON(&db.Creator{ID: cr.ID, Name: cr.Name, Notes: cr.Notes})
	m["channel_count"] = cr.ChannelCount
	return m
}

func creatorJSON(cr *db.Creator) map[string]any {
	if cr == nil {
		return nil
	}
	id := uuidString(cr.ID)
	return map[string]any{
		"kind":  "creator",
		"id":    id,
		"uri":   "rewind://creator/" + id,
		"title": cr.Name,
		"name":  cr.Name,
		"notes": cr.Notes,
	}
}

func edgeJSON(edges []*db.ChannelEdge) []map[string]any {
	out := make([]map[string]any, 0, len(edges))
	for _, e := range edges {
		if e == nil {
			continue
		}
		item := map[string]any{
			"kind":            "channel_edge",
			"id":              uuidString(e.ID),
			"from_channel_id": uuidString(e.FromChannelID),
			"to_url":          e.ToURL,
			"edge_kind":       e.Kind,
			"evidence":        e.Evidence,
			"weight":          e.Weight,
		}
		if e.ToChannelID.Valid {
			item["to_channel_id"] = uuidString(e.ToChannelID)
		}
		if e.VideoID.Valid {
			item["video_id"] = uuidString(e.VideoID)
		}
		out = append(out, item)
	}
	return out
}

func channelWebPath(uploader string) string {
	return "/channels/view?name=" + url.QueryEscape(uploader)
}

func enqueueJSON(res *archival.EnqueueResult) map[string]any {
	out := map[string]any{
		"is_playlist": res.IsPlaylist,
		"refresh":     res.Refresh,
	}
	if res.Job != nil {
		out["job_id"] = uuidString(res.Job.ID)
		out["url"] = res.Job.URL
		out["status"] = string(res.Job.Status)
	}
	return out
}

func analyzeVideo(r *db.ListVideosForAnalyzeRow) analyze.Video {
	v := analyze.Video{
		ID:           uuidString(r.ID),
		Platform:     channelid.PlatformFromSrc(r.Src),
		Format:       r.Format,
		Title:        r.Title,
		CommentCount: r.CommentCount,
	}
	if v.Format == "" {
		v.Format = "video"
	}
	if r.UploadDate.Valid {
		v.UploadDate = r.UploadDate.Time
	}
	if r.DurationSeconds != nil {
		v.DurationSeconds = int(*r.DurationSeconds)
	}
	if r.ViewCount != nil {
		v.ViewCount = *r.ViewCount
	}
	if r.LikeCount != nil {
		v.LikeCount = *r.LikeCount
	}
	return v
}

func analyzeVideoFromPage(r *db.ListVideosPaginatedRow, platform string) analyze.Video {
	v := analyze.Video{
		ID:       uuidString(r.ID),
		Platform: platform,
		Format:   r.Format,
		Title:    r.Title,
	}
	if v.Format == "" {
		v.Format = "video"
	}
	if r.UploadDate.Valid {
		v.UploadDate = r.UploadDate.Time
	}
	if r.DurationSeconds != nil {
		v.DurationSeconds = int(*r.DurationSeconds)
	}
	if r.ViewCount != nil {
		v.ViewCount = *r.ViewCount
	}
	if r.LikeCount != nil {
		v.LikeCount = *r.LikeCount
	}
	if r.Info.CommentCount > 0 {
		v.CommentCount = int64(r.Info.CommentCount)
	}
	return v
}

func similarUploaderReason(a, b string) (string, bool) {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return "", false
	}
	if strings.EqualFold(a, b) {
		return "same uploader name", true
	}
	al, bl := strings.ToLower(a), strings.ToLower(b)
	if strings.Contains(al, bl) {
		return fmt.Sprintf("%q contains %q", a, b), true
	}
	if strings.Contains(bl, al) {
		return fmt.Sprintf("%q contains %q", b, a), true
	}
	return "", false
}

func tagSlug(name string) string {
	return strings.Join(strings.Fields(strings.ToLower(name)), " ")
}
