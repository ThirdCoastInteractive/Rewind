package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"thirdcoast.systems/rewind/internal/analyze"
	"thirdcoast.systems/rewind/internal/archival"
	"thirdcoast.systems/rewind/internal/channelid"
	"thirdcoast.systems/rewind/internal/creatorlink"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/osint"
	"thirdcoast.systems/rewind/internal/videoid"
)

type filterArgs struct {
	Filter string `json:"filter,omitempty" jsonschema:"Optional name substring"`
}

type channelArgs struct {
	ID string `json:"id" jsonschema:"Channel UUID or uploader name"`
}

type urlArgs struct {
	URL   string `json:"url" jsonschema:"Channel, playlist, or video URL"`
	Label string `json:"label,omitempty" jsonschema:"Optional watch label"`
}

type indexURLArgs struct {
	URL string `json:"url" jsonschema:"YouTube video, playlist, channel, channel search, or results URL"`
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
	Query    string `json:"query" jsonschema:"Search text"`
	VideoID  string `json:"video_id,omitempty" jsonschema:"Optional video UUID"`
	Uploader string `json:"uploader,omitempty" jsonschema:"Optional uploader name (one channel)"`
	Limit    int32  `json:"limit,omitempty" jsonschema:"Max results, default 20"`
}

type channelPageArgs struct {
	ID     string `json:"id" jsonschema:"Channel UUID or uploader name"`
	Kind   string `json:"kind,omitempty" jsonschema:"Optional edge kind: outlink, mention, commented"`
	Limit  int32  `json:"limit,omitempty" jsonschema:"Max rows, default 20"`
	Offset int32  `json:"offset,omitempty"`
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
		neighbors, err := q.ListTopicNeighborsForVideo(ctx, &db.ListTopicNeighborsForVideoParams{VideoID: id, PageLimit: 20})
		if err == nil && len(neighbors) > 0 {
			rows := make([]map[string]any, 0, len(neighbors))
			seenWin := map[string]bool{}
			for _, n := range neighbors {
				if n == nil {
					continue
				}
				wid := uuidString(n.ID)
				if seenWin[wid] {
					continue
				}
				seenWin[wid] = true
				vid := uuidString(n.VideoID)
				rows = append(rows, map[string]any{
					"id":          uuidString(n.ID),
					"video_id":    vid,
					"title":       n.Title,
					"video_title": n.VideoTitle,
					"uploader":    n.Uploader,
					"start":       n.StartTs,
					"end":         n.EndTs,
					"topic_slug":  n.TopicSlug,
					"topic_title": n.TopicTitle,
					"uri":         "rewind://video/" + vid,
					"web_path":    fmt.Sprintf("/videos/%s?t=%.3f", vid, n.StartTs),
				})
			}
			out["topic_windows"] = rows
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

func getChannel(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *channelArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *channelArgs) (*mcpsdk.CallToolResult, any, error) {
		q := dbc.Queries(ctx)
		resolved, err := lookupChannel(ctx, q, args.ID)
		if err != nil {
			return nil, nil, err
		}
		out, err := channelAnalysisJSON(ctx, q, resolved, false)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(out)
	}
}

func listCreators(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *filterArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *filterArgs) (*mcpsdk.CallToolResult, any, error) {
		rows, err := dbc.Queries(ctx).ListCreators(ctx)
		if err != nil {
			return nil, nil, err
		}
		filter := strings.ToLower(strings.TrimSpace(args.Filter))
		out := make([]map[string]any, 0, len(rows))
		for _, r := range rows {
			if r == nil {
				continue
			}
			if filter != "" && !strings.Contains(strings.ToLower(r.Name), filter) {
				continue
			}
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
		out["wiki"] = wikiSummaryFor(ctx, cr.ID, pgtype.UUID{}, wikiSlug(cr.Name))
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

func analyzeChannel(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *channelArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *channelArgs) (*mcpsdk.CallToolResult, any, error) {
		q := dbc.Queries(ctx)
		resolved, err := lookupChannel(ctx, q, args.ID)
		if err != nil {
			return nil, nil, err
		}
		out, err := channelAnalysisJSON(ctx, q, resolved, true)
		if err != nil {
			return nil, nil, err
		}
		var channelID pgtype.UUID
		if resolved.Row != nil {
			channelID = resolved.Row.ID
		}
		if eng, eerr := osint.LoadChannelEngagement(ctx, dbc.Pool, resolved.uploader(), channelID); eerr == nil {
			out["comment_engagement"] = eng
		}
		return jsonResult(out)
	}
}

func indexChannelDescriptions(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *channelArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *channelArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		tok := tokenFrom(ctx)
		q := dbc.Queries(ctx)
		resolved, err := lookupChannel(ctx, q, args.ID)
		if err != nil {
			return nil, nil, err
		}
		channelURL := ""
		if resolved.Overview != nil {
			channelURL = strings.TrimSpace(resolved.Overview.ChannelURL)
		}
		if channelURL == "" && resolved.Row != nil {
			channelURL = strings.TrimSpace(resolved.Row.CanonicalURL)
		}
		if channelURL == "" {
			return nil, nil, fmt.Errorf("channel has no URL to index")
		}
		job, err := q.EnqueueMetadataCatalogJob(ctx, &db.EnqueueMetadataCatalogJobParams{
			URL:        channelURL,
			ArchivedBy: tok.UserID,
		})
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{
			"job_id":  uuidString(job.ID),
			"url":     job.URL,
			"kind":    job.Kind,
			"status":  string(job.Status),
			"channel": resolved.uploader(),
		})
	}
}

func listChannelVideos(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *channelPageArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *channelPageArgs) (*mcpsdk.CallToolResult, any, error) {
		q := dbc.Queries(ctx)
		resolved, err := lookupChannel(ctx, q, args.ID)
		if err != nil {
			return nil, nil, err
		}
		if args.Limit <= 0 || args.Limit > 50 {
			args.Limit = 20
		}
		if args.Offset < 0 {
			args.Offset = 0
		}
		params := &db.ListChannelVideosParams{
			PageOffset: args.Offset,
			PageLimit:  args.Limit,
		}
		if resolved.Row != nil {
			params.ChannelRowID = resolved.Row.ID
		} else {
			params.Uploader = nullable(resolved.uploader())
		}
		rows, err := q.ListChannelVideos(ctx, params)
		if err != nil {
			return nil, nil, err
		}
		out := make([]map[string]any, 0, len(rows))
		for _, r := range rows {
			item := map[string]any{
				"kind":     "video",
				"id":       uuidString(r.ID),
				"uri":      "rewind://video/" + uuidString(r.ID),
				"web_path": "/videos/" + uuidString(r.ID),
				"title":    r.Title,
				"uploader": r.Uploader,
				"format":   r.Format,
				"media":    r.Media,
				"src":      r.Src,
			}
			if r.UploadDate.Valid {
				item["published"] = r.UploadDate.Time.Format("2006-01-02")
			}
			if r.DurationSeconds != nil {
				item["duration_seconds"] = *r.DurationSeconds
			}
			if r.ViewCount != nil {
				item["view_count"] = *r.ViewCount
			}
			out = append(out, item)
		}
		return jsonResult(map[string]any{
			"channel": resolved.uploader(),
			"offset":  args.Offset,
			"videos":  out,
		})
	}
}

func listChannelCatalog(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *channelPageArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *channelPageArgs) (*mcpsdk.CallToolResult, any, error) {
		q := dbc.Queries(ctx)
		resolved, err := lookupChannel(ctx, q, args.ID)
		if err != nil {
			return nil, nil, err
		}
		if args.Limit <= 0 || args.Limit > 50 {
			args.Limit = 20
		}
		if args.Offset < 0 {
			args.Offset = 0
		}
		params := &db.ListChannelCatalogParams{
			PageOffset: args.Offset,
			PageLimit:  args.Limit,
		}
		if resolved.Row != nil {
			params.ChannelRowID = resolved.Row.ID
		} else {
			params.Uploader = nullable(resolved.uploader())
		}
		rows, err := q.ListChannelCatalog(ctx, params)
		if err != nil {
			return nil, nil, err
		}
		out := BuildCatalog(resolved.uploader(), rows)
		out["offset"] = args.Offset
		return jsonResult(out)
	}
}

type compareArgs struct {
	A string `json:"a" jsonschema:"First channel UUID or uploader name"`
	B string `json:"b" jsonschema:"Second channel UUID or uploader name"`
}

func compareChannels(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *compareArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *compareArgs) (*mcpsdk.CallToolResult, any, error) {
		q := dbc.Queries(ctx)
		leftRes, err := lookupChannel(ctx, q, args.A)
		if err != nil {
			return nil, nil, fmt.Errorf("channel a: %w", err)
		}
		rightRes, err := lookupChannel(ctx, q, args.B)
		if err != nil {
			return nil, nil, fmt.Errorf("channel b: %w", err)
		}
		leftVids, err := videosForResolvedChannel(ctx, q, leftRes)
		if err != nil {
			return nil, nil, err
		}
		rightVids, err := videosForResolvedChannel(ctx, q, rightRes)
		if err != nil {
			return nil, nil, err
		}
		now := time.Now()
		left := AnalysisFromReport(leftRes.uploader(), analyze.Analyze(leftVids, now), len(leftVids))
		right := AnalysisFromReport(rightRes.uploader(), analyze.Analyze(rightVids, now), len(rightVids))
		return jsonResult(CompareAnalyses(left, right))
	}
}

func listCreatorSuggestions(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *struct{}) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ *struct{}) (*mcpsdk.CallToolResult, any, error) {
		rows, err := dbc.Queries(ctx).ListPendingCreatorSuggestions(ctx)
		if err != nil {
			return nil, nil, err
		}
		out := make([]map[string]any, 0, len(rows))
		for _, r := range rows {
			if r == nil {
				continue
			}
			item := map[string]any{
				"id":            uuidString(r.ID),
				"kind":          r.Kind,
				"proposed_name": r.ProposedName,
				"reason":        r.Reason,
				"evidence":      r.Evidence,
				"status":        r.Status,
				"channel_names": r.ChannelNames,
			}
			if r.CreatorID.Valid {
				item["creator_id"] = uuidString(r.CreatorID)
			}
			out = append(out, item)
		}
		return jsonResult(out)
	}
}

func acceptCreatorSuggestion(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *idArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *idArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		id, err := parseUUID(args.ID)
		if err != nil {
			return nil, nil, err
		}
		creatorID, err := creatorlink.Accept(ctx, dbc.Queries(ctx), id)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{
			"status":     "accepted",
			"creator_id": uuidString(creatorID),
		})
	}
}

func dismissCreatorSuggestion(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *idArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *idArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		id, err := parseUUID(args.ID)
		if err != nil {
			return nil, nil, err
		}
		if err := creatorlink.Dismiss(ctx, dbc.Queries(ctx), id); err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{
			"status": "dismissed",
			"id":     args.ID,
		})
	}
}

func getChannelNeighborhood(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *channelPageArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *channelPageArgs) (*mcpsdk.CallToolResult, any, error) {
		q := dbc.Queries(ctx)
		resolved, err := lookupChannel(ctx, q, args.ID)
		if err != nil {
			return nil, nil, err
		}
		if resolved.Row == nil {
			return nil, nil, fmt.Errorf("no channel record yet; index descriptions or archive a video first")
		}
		if args.Limit <= 0 || args.Limit > 100 {
			args.Limit = 40
		}
		kind := strings.TrimSpace(strings.ToLower(args.Kind))
		switch kind {
		case "", "outlink", "mention", "commented":
		default:
			return nil, nil, fmt.Errorf("kind must be outlink, mention, or commented")
		}
		rows, err := q.ListChannelNeighborhood(ctx, &db.ListChannelNeighborhoodParams{
			ChannelID: resolved.Row.ID,
			Kind:      nullable(kind),
			PageLimit: args.Limit,
		})
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{
			"channel": resolved.uploader(),
			"kind":    kind,
			"edges":   rows,
		})
	}
}

func analyzeFollows(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *struct{}) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ *struct{}) (*mcpsdk.CallToolResult, any, error) {
		q := dbc.Queries(ctx)
		follows, err := q.ListWatchedChannels(ctx)
		if err != nil {
			return nil, nil, err
		}
		out := make([]followAnalysis, 0)
		n := 0
		for _, f := range follows {
			if f == nil || !f.Enabled {
				continue
			}
			n++
			if n > 40 {
				break
			}
			var resolved *resolvedChannel
			if f.ChannelID.Valid {
				resolved, err = lookupChannel(ctx, q, uuidString(f.ChannelID))
			} else if strings.TrimSpace(f.Label) != "" {
				resolved, err = lookupChannel(ctx, q, f.Label)
			} else {
				continue
			}
			if err != nil || resolved == nil {
				continue
			}
			videos, err := videosForResolvedChannel(ctx, q, resolved)
			if err != nil {
				return nil, nil, err
			}
			report := analyze.Analyze(videos, time.Now())
			sigs := make([]analyze.Signal, 0, 4)
			for _, s := range report.Signals {
				if s.Level == "warning" || s.Level == "bad" {
					sigs = append(sigs, s)
				}
				if len(sigs) == 4 {
					break
				}
			}
			label := f.Label
			if label == "" {
				label = resolved.uploader()
			}
			out = append(out, followAnalysis{
				Label:      label,
				Uploader:   resolved.uploader(),
				URL:        f.URL,
				Enabled:    f.Enabled,
				Status:     report.Status,
				VideoCount: len(videos),
				Signals:    sigs,
			})
		}
		sort.Slice(out, func(i, j int) bool {
			ri, rj := followStatusRank(out[i].Status), followStatusRank(out[j].Status)
			if ri != rj {
				return ri < rj
			}
			return out[i].Uploader < out[j].Uploader
		})
		return jsonResult(map[string]any{
			"follows": out,
			"count":   len(out),
		})
	}
}

type followAnalysis struct {
	Label      string           `json:"label"`
	Uploader   string           `json:"uploader"`
	URL        string           `json:"url"`
	Enabled    bool             `json:"enabled"`
	Status     string           `json:"status"`
	VideoCount int              `json:"video_count"`
	Signals    []analyze.Signal `json:"signals"`
}

func followStatusRank(status string) int {
	switch status {
	case "dying":
		return 0
	case "drifting":
		return 1
	default:
		return 2
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
		if strings.TrimSpace(args.Query) == "" {
			return jsonResult([]citation{})
		}
		if args.Limit <= 0 || args.Limit > 50 {
			args.Limit = 20
		}
		params := &db.SearchCommentsScopedParams{
			Query:      args.Query,
			PageOffset: 0,
			PageSize:   args.Limit,
			Uploader:   nullable(args.Uploader),
		}
		if strings.TrimSpace(args.VideoID) != "" {
			vid, err := parseUUID(args.VideoID)
			if err != nil {
				return nil, nil, err
			}
			params.VideoID = vid
		}
		if up := strings.TrimSpace(args.Uploader); up != "" {
			if resolved, err := lookupChannel(ctx, dbc.Queries(ctx), up); err == nil && resolved != nil {
				params.Uploader = nullable(resolved.uploader())
			}
		}
		rows, err := dbc.Queries(ctx).SearchCommentsScoped(ctx, params)
		if err != nil {
			return nil, nil, err
		}
		out := make([]citation, 0, len(rows))
		for _, r := range rows {
			sid := uuidString(r.VideoID)
			quote := ""
			if r.Text != nil {
				quote = *r.Text
			}
			title := r.Title
			if r.Author != nil && *r.Author != "" {
				title = *r.Author
			}
			out = append(out, citation{
				Kind:     "comment",
				ID:       uuidString(r.ID),
				URI:      "rewind://video/" + sid,
				WebPath:  "/videos/" + sid,
				Title:    title,
				Quote:    quote,
				VideoID:  sid,
				Uploader: r.Uploader,
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

const indexURLNextAction = "This indexes titles and English YouTube captions only; it does not download media. Poll get_index_status(job_id), not get_transcription_status. After complete, use find_clip_candidates or search_library. Catalog-only rows cannot play until enqueue_download."

func indexURL(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *indexURLArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *indexURLArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		raw := strings.TrimSpace(args.URL)
		if raw == "" {
			return nil, nil, fmt.Errorf("url is required")
		}
		tok := tokenFrom(ctx)
		collection := videoid.IsPlaylistOrChannelURL(raw)
		// EnqueueCatalogMetadataJobs does not return a job row. A metadata-catalog
		// parent fans out skip-download metadata children (one entry for a watch URL).
		job, err := dbc.Queries(ctx).EnqueueMetadataCatalogJob(ctx, &db.EnqueueMetadataCatalogJobParams{
			URL:        raw,
			ArchivedBy: tok.UserID,
		})
		if err != nil {
			return nil, nil, err
		}

		return jsonResult(map[string]any{
			"job_id":      uuidString(job.ID),
			"url":         job.URL,
			"kind":        job.Kind,
			"status":      string(job.Status),
			"collection":  collection,
			"next_action": indexURLNextAction,
			"next_tool":   "get_index_status",
		})
	}
}

type indexStatusArgs struct {
	JobID string `json:"job_id" jsonschema:"Job UUID from index_url, index_channel_catalog, or index_creator_catalog"`
}

func getIndexStatus(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *indexStatusArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *indexStatusArgs) (*mcpsdk.CallToolResult, any, error) {
		if tokenFrom(ctx) == nil {
			return nil, nil, fmt.Errorf("authentication required")
		}
		id, err := parseUUID(args.JobID)
		if err != nil {
			return nil, nil, err
		}
		out, err := indexJobJSON(ctx, dbc.Queries(ctx), id)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(out)
	}
}

func indexJobJSON(ctx context.Context, q *db.Queries, id pgtype.UUID) (map[string]any, error) {
	job, err := q.GetDownloadJobByID(ctx, id)
	if err == nil {
		out := map[string]any{
			"job_id":      uuidString(job.ID),
			"kind":        job.Kind,
			"status":      string(job.Status),
			"url":         job.URL,
			"next_action": indexStatusNextAction(string(job.Status)),
			"next_tool":   "get_index_status",
		}
		if job.LastError != nil && *job.LastError != "" {
			out["last_error"] = *job.LastError
		}
		if job.BatchTotal != nil {
			out["batch_total"] = *job.BatchTotal
		}
		if job.VideoID.Valid {
			out["video_id"] = uuidString(job.VideoID)
		}
		if doneIndexStatus(string(job.Status)) {
			out["next_tool"] = "find_clip_candidates"
		}
		return out, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	crawl, err := q.GetCatalogCrawl(ctx, id)
	if err == nil {
		out := map[string]any{
			"job_id":          uuidString(crawl.ID),
			"kind":            "catalog-crawl",
			"status":          crawl.Status,
			"url":             crawl.FeedURL,
			"feed_kind":       crawl.FeedKind,
			"entries_seen":    crawl.EntriesSeen,
			"entries_added":   crawl.EntriesAdded,
			"entries_updated": crawl.EntriesUpdated,
			"last_error":      crawl.LastError,
			"next_action":     indexStatusNextAction(crawl.Status),
			"next_tool":       "get_index_status",
		}
		if crawl.ChannelID.Valid {
			out["channel_id"] = uuidString(crawl.ChannelID)
		}
		if doneIndexStatus(crawl.Status) {
			out["next_tool"] = "find_clip_candidates"
		}
		return out, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	ml, err := q.GetMLJob(ctx, id)
	if err == nil {
		return map[string]any{
			"job_id":      uuidString(ml.ID),
			"kind":        ml.Kind,
			"status":      ml.Status,
			"next_action": "This is an ML job, not caption ingest. Use get_transcription_status for transcribe jobs.",
			"next_tool":   "get_transcription_status",
		}, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("unknown job_id")
	}
	return nil, err
}

func doneIndexStatus(status string) bool {
	switch status {
	case "succeeded", "complete", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func indexStatusNextAction(status string) string {
	switch status {
	case "succeeded", "complete":
		return "Captions are indexed. Call find_clip_candidates or search_library. Catalog-only rows cannot play until enqueue_download."
	case "failed", "cancelled":
		return "Indexing failed. Inspect last_error; retry index_url or index_channel_catalog if needed."
	default:
		return "Indexing still running. Poll get_index_status with the same job_id. Do not call get_transcription_status."
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
			if n, err := q.DeleteOwnedWatchedChannel(ctx, &db.DeleteOwnedWatchedChannelParams{ID: id, CreatedBy: tokenFrom(ctx).UserID}); err != nil || n != 1 {
				return nil, nil, fmt.Errorf("watch not found or access denied")
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
			if !r.CreatedBy.Valid || uuidString(r.CreatedBy) != uuidString(tok.UserID) {
				continue
			}
			if n, err := q.DeleteOwnedWatchedChannel(ctx, &db.DeleteOwnedWatchedChannelParams{ID: r.ID, CreatedBy: tok.UserID}); err != nil || n != 1 {
				return nil, nil, fmt.Errorf("watch not found or access denied")
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

func proposeRundownPrompt(_ context.Context, req *mcpsdk.GetPromptRequest) (*mcpsdk.GetPromptResult, error) {
	topic := ""
	if req != nil && req.Params != nil && req.Params.Arguments != nil {
		topic = req.Params.Arguments["topic"]
	}
	var b strings.Builder
	b.WriteString("Build a rundown from Rewind archive evidence")
	if topic != "" {
		fmt.Fprintf(&b, " about %q", topic)
	}
	b.WriteString(".\n")
	b.WriteString("1. Search with search_transcripts and/or search_visual_moments. Similarity scores are not labels.\n")
	b.WriteString("2. Inspect matching cues (get_transcript) and images (get_video_frames or get_video_contact_sheet) before describing them.\n")
	b.WriteString("3. Propose an ordered rundown with propose_show_note_patch. Do not approve it; wait_show_note_events until a human accepts.\n")
	b.WriteString("4. Only after acceptance, call create_compilation with the current plan revision. Do not render without an explicit request.\n")
	return &mcpsdk.GetPromptResult{
		Description: "Search, inspect, propose, wait for review, then compile.",
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

type resolvedChannel struct {
	Row      *db.Channel
	Overview *db.GetChannelOverviewRow
}

func (r *resolvedChannel) uploader() string {
	if r == nil {
		return ""
	}
	if r.Row != nil && strings.TrimSpace(r.Row.Uploader) != "" {
		return r.Row.Uploader
	}
	if r.Overview != nil {
		return r.Overview.Uploader
	}
	return ""
}

func lookupChannel(ctx context.Context, q *db.Queries, raw string) (*resolvedChannel, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("channel id or uploader required")
	}
	if id, err := parseUUID(raw); err == nil {
		ch, err := q.GetChannel(ctx, id)
		if err != nil || ch == nil {
			return nil, fmt.Errorf("channel not found")
		}
		ov, _ := q.GetChannelOverview(ctx, ch.Uploader)
		return &resolvedChannel{Row: ch, Overview: ov}, nil
	}
	ov, err := q.GetChannelOverview(ctx, raw)
	if err != nil || ov == nil {
		return nil, fmt.Errorf("channel not found")
	}
	return &resolvedChannel{Row: channelFromOverview(ctx, q, ov), Overview: ov}, nil
}

func channelFromOverview(ctx context.Context, q *db.Queries, ov *db.GetChannelOverviewRow) *db.Channel {
	if ov == nil {
		return nil
	}
	for _, raw := range []string{ov.ChannelURL, ov.UploaderURL} {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		id := channelid.FromURL(raw)
		if id.Key == "" || id.Key == "unknown" {
			continue
		}
		ch, err := q.GetChannelByIdentity(ctx, &db.GetChannelByIdentityParams{
			Platform:    id.Platform,
			IdentityKey: id.Key,
		})
		if err == nil && ch != nil {
			return ch
		}
	}
	return nil
}

func channelAnalysisJSON(ctx context.Context, q *db.Queries, resolved *resolvedChannel, runAnalyze bool) (map[string]any, error) {
	up := resolved.uploader()
	out := map[string]any{
		"kind":     "channel",
		"title":    up,
		"uploader": up,
		"uri":      "rewind://channel/" + url.PathEscape(up),
		"web_path": channelWebPath(up),
	}
	if resolved.Row != nil {
		for k, v := range channelRowJSON(resolved.Row) {
			out[k] = v
		}
		if resolved.Row.CreatorID.Valid {
			if cr, err := q.GetCreator(ctx, resolved.Row.CreatorID); err == nil && cr != nil {
				out["creator"] = creatorJSON(cr)
			}
		}
		edges, err := q.ListChannelEdgesForChannel(ctx, resolved.Row.ID)
		if err != nil {
			return nil, err
		}
		out["edges"] = edges
	}
	if ov := resolved.Overview; ov != nil {
		out["video_count"] = ov.VideoCount
		out["total_duration_seconds"] = ov.TotalDurationSeconds
		out["total_size_bytes"] = ov.TotalSizeBytes
		out["total_views"] = ov.TotalViews
		out["channel_url"] = ov.ChannelURL
		out["uploader_url"] = ov.UploaderURL
		out["watch_id"] = uuidString(ov.WatchID)
		if ov.LatestUpload.Valid {
			out["latest_upload"] = ov.LatestUpload.Time.Format("2006-01-02")
		}
	}
	homeHint := ""
	if resolved.Row != nil {
		homeHint = wikiSlug(resolved.Row.Uploader)
		out["wiki"] = wikiSummaryFor(ctx, resolved.Row.CreatorID, resolved.Row.ID, homeHint)
	} else if _, ok := out["wiki"]; !ok {
		out["wiki"] = nil
	}
	if !runAnalyze {
		return out, nil
	}
	videos, err := videosForResolvedChannel(ctx, q, resolved)
	if err != nil {
		return nil, err
	}
	report := analyze.Analyze(videos, time.Now())
	out["analyzed_video_count"] = len(videos)
	out["status"] = report.Status
	out["signals"] = report.Signals
	out["stats"] = report.Stats
	out["old_format_mix"] = report.OldFormatMix
	out["recent_format_mix"] = report.RecentFormatMix
	return out, nil
}

func videosForResolvedChannel(ctx context.Context, q *db.Queries, resolved *resolvedChannel) ([]analyze.Video, error) {
	if resolved.Row != nil {
		rows, err := q.ListVideosForAnalyze(ctx, []pgtype.UUID{resolved.Row.ID})
		if err != nil {
			return nil, err
		}
		if len(rows) > 0 {
			videos := make([]analyze.Video, 0, len(rows))
			for _, r := range rows {
				videos = append(videos, analyzeVideo(r))
			}
			return videos, nil
		}
	}
	up := resolved.uploader()
	if up == "" {
		return nil, nil
	}
	page, err := q.ListVideosPaginated(ctx, &db.ListVideosPaginatedParams{
		Uploader:   &up,
		SortOrder:  "published-newest",
		PageOffset: 0,
		PageLimit:  200,
	})
	if err != nil {
		return nil, err
	}
	platform := ""
	if resolved.Row != nil {
		platform = resolved.Row.Platform
	}
	videos := make([]analyze.Video, 0, len(page))
	for _, r := range page {
		videos = append(videos, analyzeVideoFromPage(r, platform))
	}
	return videos, nil
}

func analyzeChannelPrompt(_ context.Context, req *mcpsdk.GetPromptRequest) (*mcpsdk.GetPromptResult, error) {
	channel := ""
	if req != nil && req.Params != nil && req.Params.Arguments != nil {
		channel = req.Params.Arguments["channel"]
	}
	var b strings.Builder
	b.WriteString("Analyze this archived Rewind channel")
	if channel != "" {
		fmt.Fprintf(&b, ": %q", channel)
	}
	b.WriteString(".\n")
	b.WriteString("Call analyze_channel with the uploader name or channel UUID.\n")
	b.WriteString("Summarize status and the important signals (views/day, cadence, format mix).\n")
	b.WriteString("Use the returned edges to name who they outlink and mention.\n")
	b.WriteString("If there is a creator_id, optionally call get_creator or analyze_creator for the rest of the person.")
	return &mcpsdk.GetPromptResult{
		Description: "Analyze an archived channel's trajectory and relationships.",
		Messages: []*mcpsdk.PromptMessage{{
			Role:    "user",
			Content: &mcpsdk.TextContent{Text: b.String()},
		}},
	}, nil
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

func channelWebPath(uploader string) string {
	return "/channels/view?name=" + url.QueryEscape(uploader)
}

func enqueueJSON(res *archival.EnqueueResult) map[string]any {
	out := map[string]any{
		"is_playlist": res.IsPlaylist,
		"refresh":     res.Refresh,
		"reused":      res.Reused,
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
		ID:           uuidString(r.ID),
		Platform:     platform,
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
