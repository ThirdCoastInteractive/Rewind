package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"thirdcoast.systems/rewind/internal/comments"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/osint"
	"thirdcoast.systems/rewind/internal/textcls"
	"thirdcoast.systems/rewind/internal/videoid"
	"thirdcoast.systems/rewind/pkg/plugin"
	"thirdcoast.systems/rewind/pkg/ytdlp"
)

const osintWorkflow = `Archive OSINT desk for public posts already in Rewind. Start with get_osint_workflow, then search_commenters, open get_commenter dossiers, list_commenter_comments for evidence, and list_osint_flags / list_campaigns / get_campaign for open signals. Use get_video_comment_tone and get_video_speech_tone for per-video rollups. Optional writes (mcp:write): watch_commenter / unwatch_commenter, dismiss_osint_flag, assert_commenter_link (operator assertion only), enqueue_comment_classify / enqueue_speech_tone.

Rules: public posts only. Commenters are keyed by (source, author_id), not real-world identity. The desk is observe-and-flag only; do not moderate or take enforcement actions. Style neighbors and sock suggestions are hypotheses only. Never look up real-world PII. These tools do not identify real-world persons. Channel comment engagement (get_channel_comment_engagement) is harvested comments per 1k views with raids/copypaste and open sock flags split out; organic is not proof of genuine audience.

Tool sequence:
1. get_osint_workflow
2. search_commenters (optional watchlisted/flagged filters)
3. get_commenter
4. list_commenter_comments
5. list_osint_flags / list_campaigns / get_campaign as needed
6. get_video_comment_tone / get_video_speech_tone for video context
7. get_channel_comment_engagement for organic vs inflated comment rates
8. watch_commenter, dismiss_osint_flag, assert_commenter_link, enqueue_*, or index_x_replies only with mcp:write`

func registerOSINTTools(srv *mcpsdk.Server, dbc *db.DatabaseConnection) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "get_osint_workflow",
		Description: "Start here for the archive OSINT desk. Public posts only; observe and flag; no moderation or real-world person identification.",
	}, func(context.Context, *mcpsdk.CallToolRequest, *struct{}) (*mcpsdk.CallToolResult, any, error) {
		return jsonResult(map[string]any{"workflow": osintWorkflow})
	})
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "search_commenters",
		Description: "Search archived commenters by display name, author_id, or author_url. Optional watchlisted/flagged filters. Observe-only; does not moderate or identify real-world persons.",
	}, searchCommentersMCP(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "get_commenter",
		Description: "Return a commenter dossier (identity, score rollup, flags, campaigns, style neighbors, evidence URIs). Not a citation. Observe-only.",
	}, getCommenterMCP(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "list_commenter_comments",
		Description: "Paginated comment citations (kind=comment) for one commenter.",
	}, listCommenterCommentsMCP(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "list_osint_flags",
		Description: "List OSINT flags. Optional kind; open-only by default. Flags are observe signals, not enforcement.",
	}, listOSINTFlagsMCP(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "list_campaigns",
		Description: "List detected campaigns (copypaste/raid). Observe-only.",
	}, listCampaignsMCP(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "get_campaign",
		Description: "Get one campaign by UUID with member counts and evidence.",
	}, getCampaignMCP(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "get_video_comment_tone",
		Description: "Per-video comment score rollup plus up to 10 sample scored comments.",
	}, getVideoCommentToneMCP(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "get_video_speech_tone",
		Description: "Window-aligned speech tone scores for a video.",
	}, getVideoSpeechToneMCP(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "watch_commenter",
		Description: "Add a commenter to the caller's watchlist. Requires mcp:write. Observe-only; does not moderate.",
	}, watchCommenterMCP(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "unwatch_commenter",
		Description: "Remove a commenter from the caller's watchlist. Requires mcp:write.",
	}, unwatchCommenterMCP(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "dismiss_osint_flag",
		Description: "Dismiss an open OSINT flag. Requires mcp:write. Does not moderate or enforce.",
	}, dismissOSINTFlagMCP(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "assert_commenter_link",
		Description: "Operator assertion that two commenters are linked (kind=user). Ordered ids. Requires mcp:write. Hypothesis/assertion only.",
	}, assertCommenterLinkMCP(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "enqueue_comment_classify",
		Description: "Enqueue ML job kind=comment_classify for a video. Requires mcp:write.",
	}, enqueueCommentClassifyMCP(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "enqueue_speech_tone",
		Description: "Enqueue ML job kind=speech_tone for a video. Requires mcp:write.",
	}, enqueueSpeechToneMCP(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "index_x_replies",
		Description: "Index public replies on an archived X status URL via yt-dlp. Empty extractor output fails visibly. Requires mcp:write. No DMs or firehose.",
	}, indexXRepliesMCP(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "get_channel_comment_engagement",
		Description: "Harvested comment engagement for a channel: comments per 1k views, organic vs raid/copypaste/sock-suspect. Sock flags are observations, not proof. Observe-only.",
	}, getChannelCommentEngagementMCP(dbc))
}

type searchCommentersArgs struct {
	Query       string `json:"query" jsonschema:"Display name, author_id, or author_url substring"`
	Watchlisted *bool  `json:"watchlisted,omitempty" jsonschema:"If true, only commenters on the caller's watchlist"`
	Flagged     *bool  `json:"flagged,omitempty" jsonschema:"If true, only commenters with an open OSINT flag"`
	Limit       int32  `json:"limit,omitempty" jsonschema:"Max results, default 20, max 50"`
}

type commenterIDArgs struct {
	CommenterID string `json:"commenter_id" jsonschema:"Commenter UUID"`
}

type listCommenterCommentsArgs struct {
	CommenterID string `json:"commenter_id" jsonschema:"Commenter UUID"`
	Limit       int32  `json:"limit,omitempty" jsonschema:"Page size, default 20, max 50"`
	Offset      int32  `json:"offset,omitempty" jsonschema:"Page offset"`
}

type listOSINTFlagsArgs struct {
	Kind     string `json:"kind,omitempty" jsonschema:"Optional flag kind: campaign, raid, newcomer, sock_suggest, toxicity_burst"`
	OpenOnly *bool  `json:"open_only,omitempty" jsonschema:"Default true: only undismissed flags"`
	Limit    int32  `json:"limit,omitempty" jsonschema:"Max rows, default 50, max 100"`
	Offset   int32  `json:"offset,omitempty"`
}

type listCampaignsArgs struct {
	Limit  int32 `json:"limit,omitempty" jsonschema:"Max rows, default 50, max 100"`
	Offset int32 `json:"offset,omitempty"`
}

type campaignIDArgs struct {
	CampaignID string `json:"campaign_id" jsonschema:"Campaign UUID"`
}

type videoIDArgs struct {
	VideoID string `json:"video_id" jsonschema:"Video UUID"`
}

type watchCommenterArgs struct {
	CommenterID string `json:"commenter_id" jsonschema:"Commenter UUID"`
	Note        string `json:"note,omitempty" jsonschema:"Optional operator note"`
}

type dismissOSINTFlagArgs struct {
	FlagID string `json:"flag_id" jsonschema:"OSINT flag UUID"`
}

type assertCommenterLinkArgs struct {
	A string `json:"a_id" jsonschema:"First commenter UUID"`
	B string `json:"b_id" jsonschema:"Second commenter UUID"`
}

func searchCommentersMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *searchCommentersArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *searchCommentersArgs) (*mcpsdk.CallToolResult, any, error) {
		if strings.TrimSpace(a.Query) == "" {
			return jsonResult(map[string]any{"commenters": []any{}})
		}
		limit := a.Limit
		if limit <= 0 {
			limit = 20
		}
		if limit > 50 {
			limit = 50
		}
		if dbc == nil {
			return nil, nil, fmt.Errorf("database unavailable")
		}
		var userID pgtype.UUID
		watchlisted := a.Watchlisted != nil && *a.Watchlisted
		flagged := a.Flagged != nil && *a.Flagged
		if watchlisted {
			tok := tokenFrom(ctx)
			if tok == nil {
				return nil, nil, fmt.Errorf("authentication required")
			}
			userID = tok.UserID
		}
		q := `
SELECT c.id, c.source, c.author_id, c.author_url, c.display_name, c.comment_count, c.first_seen, c.last_seen, c.channel_id,
       GREATEST(
         similarity(c.display_name, $1),
         similarity(c.author_id, $1),
         similarity(c.author_url, $1)
       )::float4 AS score
FROM commenters c
WHERE (
  c.display_name ILIKE '%' || $1 || '%'
  OR c.author_id ILIKE '%' || $1 || '%'
  OR c.author_url ILIKE '%' || $1 || '%'
  OR similarity(c.display_name, $1) > 0.3
)`
		args := []any{strings.TrimSpace(a.Query)}
		argn := 2
		if watchlisted {
			q += fmt.Sprintf(`
  AND EXISTS (
    SELECT 1 FROM commenter_watchlist w
    WHERE w.commenter_id = c.id AND w.user_id = $%d
  )`, argn)
			args = append(args, userID)
			argn++
		}
		if flagged {
			q += `
  AND EXISTS (
    SELECT 1 FROM osint_flags f
    WHERE f.commenter_id = c.id AND f.dismissed_at IS NULL
  )`
		}
		q += fmt.Sprintf(`
ORDER BY score DESC, c.comment_count DESC, c.display_name
LIMIT $%d`, argn)
		args = append(args, limit)

		rows, err := dbc.Query(ctx, q, args...)
		if err != nil {
			return nil, nil, err
		}
		defer rows.Close()
		out := make([]map[string]any, 0)
		for rows.Next() {
			var (
				id, channelID                        pgtype.UUID
				source, authorID, authorURL, display string
				commentCount                         int64
				firstSeen, lastSeen                  time.Time
				score                                float32
			)
			if err := rows.Scan(&id, &source, &authorID, &authorURL, &display, &commentCount, &firstSeen, &lastSeen, &channelID, &score); err != nil {
				return nil, nil, err
			}
			item := map[string]any{
				"id":            uuidString(id),
				"source":        source,
				"author_id":     authorID,
				"author_url":    authorURL,
				"display_name":  display,
				"comment_count": commentCount,
				"first_seen":    firstSeen,
				"last_seen":     lastSeen,
				"score":         score,
			}
			if channelID.Valid {
				item["channel_id"] = uuidString(channelID)
			}
			out = append(out, item)
		}
		if err := rows.Err(); err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{"commenters": out})
	}
}

func getCommenterMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *commenterIDArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *commenterIDArgs) (*mcpsdk.CallToolResult, any, error) {
		id, err := parseUUID(a.CommenterID)
		if err != nil {
			return nil, nil, err
		}
		if dbc == nil {
			return nil, nil, fmt.Errorf("database unavailable")
		}
		var (
			source, authorID, authorURL, display string
			commentCount                         int64
			styleN                               int32
			firstSeen, lastSeen, createdAt       time.Time
			channelID                            pgtype.UUID
			styleFeatures                        []byte
			simhash                              *int64
		)
		err = dbc.QueryRow(ctx, `
SELECT source, author_id, author_url, display_name, comment_count, first_seen, last_seen, created_at,
       channel_id, style_features, style_n, simhash
FROM commenters WHERE id = $1`, id).Scan(
			&source, &authorID, &authorURL, &display, &commentCount, &firstSeen, &lastSeen, &createdAt,
			&channelID, &styleFeatures, &styleN, &simhash,
		)
		if err != nil {
			if err == pgx.ErrNoRows {
				return nil, nil, fmt.Errorf("commenter not found")
			}
			return nil, nil, err
		}
		identity := map[string]any{
			"id":            uuidString(id),
			"source":        source,
			"author_id":     authorID,
			"author_url":    authorURL,
			"display_name":  display,
			"comment_count": commentCount,
			"first_seen":    firstSeen,
			"last_seen":     lastSeen,
			"created_at":    createdAt,
			"style_n":       styleN,
		}
		if channelID.Valid {
			identity["channel_id"] = uuidString(channelID)
		}
		if len(styleFeatures) > 0 {
			var features any
			if json.Unmarshal(styleFeatures, &features) == nil {
				identity["style_features"] = features
			}
		}
		if simhash != nil {
			identity["simhash"] = *simhash
		}

		var scoredCount int64
		var avgSentiment, avgToxicity *float64
		_ = dbc.QueryRow(ctx, `
SELECT COUNT(s.comment_id)::bigint, AVG(s.sentiment)::float8, AVG(s.toxicity)::float8
FROM video_comments c
JOIN comment_scores s ON s.comment_id = c.id
WHERE c.commenter_id = $1`, id).Scan(&scoredCount, &avgSentiment, &avgToxicity)
		scores := map[string]any{"scored_count": scoredCount}
		if avgSentiment != nil {
			scores["avg_sentiment"] = *avgSentiment
		}
		if avgToxicity != nil {
			scores["avg_toxicity"] = *avgToxicity
		}

		flags, err := queryOSINTFlags(ctx, dbc, "", true, 50, 0, &id)
		if err != nil {
			return nil, nil, err
		}
		campaigns, err := queryCommenterCampaigns(ctx, dbc, id)
		if err != nil {
			return nil, nil, err
		}
		neighbors, err := queryStyleNeighbors(ctx, dbc, id)
		if err != nil {
			return nil, nil, err
		}
		evidence, err := queryCommenterEvidence(ctx, dbc, id, 10)
		if err != nil {
			return nil, nil, err
		}

		return jsonResult(map[string]any{
			"identity":        identity,
			"scores":          scores,
			"flags":           flags,
			"campaigns":       campaigns,
			"style_neighbors": neighbors,
			"evidence":        evidence,
			"note":            "Dossier is archive observation only; style matches are hypotheses; not a citation and not real-world identification.",
		})
	}
}

func listCommenterCommentsMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *listCommenterCommentsArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *listCommenterCommentsArgs) (*mcpsdk.CallToolResult, any, error) {
		id, err := parseUUID(a.CommenterID)
		if err != nil {
			return nil, nil, err
		}
		limit := a.Limit
		if limit <= 0 {
			limit = 20
		}
		if limit > 50 {
			limit = 50
		}
		if a.Offset < 0 {
			a.Offset = 0
		}
		if dbc == nil {
			return nil, nil, fmt.Errorf("database unavailable")
		}
		rows, err := dbc.Query(ctx, `
SELECT c.id, c.video_id, c.author, c.text, v.title, v.uploader
FROM video_comments c
JOIN videos v ON v.id = c.video_id
WHERE c.commenter_id = $1
ORDER BY c.published_at DESC NULLS LAST, c.created_at DESC
LIMIT $2 OFFSET $3`, id, limit, a.Offset)
		if err != nil {
			return nil, nil, err
		}
		defer rows.Close()
		out := make([]citation, 0)
		for rows.Next() {
			var (
				cid, vid        pgtype.UUID
				author, text    *string
				title, uploader string
			)
			if err := rows.Scan(&cid, &vid, &author, &text, &title, &uploader); err != nil {
				return nil, nil, err
			}
			sid := uuidString(vid)
			quote := ""
			if text != nil {
				quote = *text
			}
			ctitle := title
			if author != nil && *author != "" {
				ctitle = *author
			}
			out = append(out, citation{
				Kind:     "comment",
				ID:       uuidString(cid),
				URI:      "rewind://video/" + sid,
				WebPath:  "/videos/" + sid,
				Title:    ctitle,
				Quote:    quote,
				VideoID:  sid,
				Uploader: uploader,
			})
		}
		if err := rows.Err(); err != nil {
			return nil, nil, err
		}
		return jsonResult(out)
	}
}

func listOSINTFlagsMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *listOSINTFlagsArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *listOSINTFlagsArgs) (*mcpsdk.CallToolResult, any, error) {
		openOnly := true
		if a.OpenOnly != nil {
			openOnly = *a.OpenOnly
		}
		limit := a.Limit
		if limit <= 0 {
			limit = 50
		}
		if limit > 100 {
			limit = 100
		}
		if a.Offset < 0 {
			a.Offset = 0
		}
		if dbc == nil {
			return nil, nil, fmt.Errorf("database unavailable")
		}
		flags, err := queryOSINTFlags(ctx, dbc, strings.TrimSpace(a.Kind), openOnly, limit, a.Offset, nil)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{"flags": flags})
	}
}

func listCampaignsMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *listCampaignsArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *listCampaignsArgs) (*mcpsdk.CallToolResult, any, error) {
		limit := a.Limit
		if limit <= 0 {
			limit = 50
		}
		if limit > 100 {
			limit = 100
		}
		if a.Offset < 0 {
			a.Offset = 0
		}
		if dbc == nil {
			return nil, nil, fmt.Errorf("database unavailable")
		}
		rows, err := dbc.Query(ctx, `
SELECT id, kind, simhash, normalized_text, first_seen, last_seen, comment_count, commenter_count, video_count, evidence, created_at, updated_at
FROM campaigns
ORDER BY last_seen DESC, created_at DESC
LIMIT $1 OFFSET $2`, limit, a.Offset)
		if err != nil {
			return nil, nil, err
		}
		defer rows.Close()
		out := make([]map[string]any, 0)
		for rows.Next() {
			item, err := scanCampaign(rows)
			if err != nil {
				return nil, nil, err
			}
			out = append(out, item)
		}
		if err := rows.Err(); err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{"campaigns": out})
	}
}

func getCampaignMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *campaignIDArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *campaignIDArgs) (*mcpsdk.CallToolResult, any, error) {
		id, err := parseUUID(a.CampaignID)
		if err != nil {
			return nil, nil, err
		}
		if dbc == nil {
			return nil, nil, fmt.Errorf("database unavailable")
		}
		row, err := dbc.Query(ctx, `
SELECT id, kind, simhash, normalized_text, first_seen, last_seen, comment_count, commenter_count, video_count, evidence, created_at, updated_at
FROM campaigns WHERE id = $1`, id)
		if err != nil {
			return nil, nil, err
		}
		defer row.Close()
		if !row.Next() {
			return nil, nil, fmt.Errorf("campaign not found")
		}
		item, err := scanCampaign(row)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(item)
	}
}

func getVideoCommentToneMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *videoIDArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *videoIDArgs) (*mcpsdk.CallToolResult, any, error) {
		id, err := parseUUID(a.VideoID)
		if err != nil {
			return nil, nil, err
		}
		if dbc == nil {
			return nil, nil, fmt.Errorf("database unavailable")
		}
		var scoredCount int64
		var avgSentiment, avgToxicity *float64
		if err := dbc.QueryRow(ctx, `
SELECT COUNT(s.comment_id)::bigint, AVG(s.sentiment)::float8, AVG(s.toxicity)::float8
FROM video_comments c
JOIN comment_scores s ON s.comment_id = c.id
WHERE c.video_id = $1`, id).Scan(&scoredCount, &avgSentiment, &avgToxicity); err != nil {
			return nil, nil, err
		}
		rollup := map[string]any{"video_id": a.VideoID, "scored_count": scoredCount}
		if avgSentiment != nil {
			rollup["avg_sentiment"] = *avgSentiment
		}
		if avgToxicity != nil {
			rollup["avg_toxicity"] = *avgToxicity
		}
		rows, err := dbc.Query(ctx, `
SELECT c.id, c.author, c.text, s.sentiment, s.toxicity, s.labels
FROM video_comments c
JOIN comment_scores s ON s.comment_id = c.id
WHERE c.video_id = $1
ORDER BY c.published_at DESC NULLS LAST, c.created_at DESC
LIMIT 10`, id)
		if err != nil {
			return nil, nil, err
		}
		defer rows.Close()
		samples := make([]map[string]any, 0)
		sid := strings.TrimSpace(a.VideoID)
		for rows.Next() {
			var (
				cid            pgtype.UUID
				author, text   *string
				sentiment, tox *float64
				labels         []byte
			)
			if err := rows.Scan(&cid, &author, &text, &sentiment, &tox, &labels); err != nil {
				return nil, nil, err
			}
			item := map[string]any{
				"id":       uuidString(cid),
				"uri":      "rewind://video/" + sid,
				"web_path": "/videos/" + sid,
				"video_id": sid,
			}
			if author != nil {
				item["author"] = *author
			}
			if text != nil {
				item["text"] = *text
			}
			if sentiment != nil {
				item["sentiment"] = *sentiment
			}
			if tox != nil {
				item["toxicity"] = *tox
			}
			if len(labels) > 0 {
				var lab any
				if json.Unmarshal(labels, &lab) == nil {
					item["labels"] = lab
				}
			}
			samples = append(samples, item)
		}
		if err := rows.Err(); err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{"rollup": rollup, "samples": samples})
	}
}

func getVideoSpeechToneMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *videoIDArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *videoIDArgs) (*mcpsdk.CallToolResult, any, error) {
		id, err := parseUUID(a.VideoID)
		if err != nil {
			return nil, nil, err
		}
		if dbc == nil {
			return nil, nil, fmt.Errorf("database unavailable")
		}
		rows, err := dbc.Query(ctx, `
SELECT id, set_id, start_ts, end_ts, model_digest, sentiment, toxicity, labels, scored_at
FROM speech_scores
WHERE video_id = $1
ORDER BY start_ts, end_ts, scored_at DESC`, id)
		if err != nil {
			return nil, nil, err
		}
		defer rows.Close()
		out := make([]map[string]any, 0)
		for rows.Next() {
			var (
				sid, setID          pgtype.UUID
				startTS, endTS      float64
				modelDigest         string
				sentiment, toxicity *float64
				labels              []byte
				scoredAt            time.Time
			)
			if err := rows.Scan(&sid, &setID, &startTS, &endTS, &modelDigest, &sentiment, &toxicity, &labels, &scoredAt); err != nil {
				return nil, nil, err
			}
			item := map[string]any{
				"id":        uuidString(sid),
				"video_id":  a.VideoID,
				"start_ts":  startTS,
				"end_ts":    endTS,
				"scored_at": scoredAt,
			}
			if setID.Valid {
				item["set_id"] = uuidString(setID)
			}
			if modelDigest != "" {
				item["model_digest"] = modelDigest
			}
			if sentiment != nil {
				item["sentiment"] = *sentiment
			}
			if toxicity != nil {
				item["toxicity"] = *toxicity
			}
			if len(labels) > 0 {
				var lab any
				if json.Unmarshal(labels, &lab) == nil {
					item["labels"] = lab
				}
			}
			out = append(out, item)
		}
		if err := rows.Err(); err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{"video_id": a.VideoID, "scores": out})
	}
}

func watchCommenterMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *watchCommenterArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *watchCommenterArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		cid, err := parseUUID(a.CommenterID)
		if err != nil {
			return nil, nil, err
		}
		if dbc == nil {
			return nil, nil, fmt.Errorf("database unavailable")
		}
		_, err = dbc.Exec(ctx, `
INSERT INTO commenter_watchlist (user_id, commenter_id, note)
VALUES ($1, $2, $3)
ON CONFLICT (user_id, commenter_id)
DO UPDATE SET note = EXCLUDED.note`, tokenFrom(ctx).UserID, cid, a.Note)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{"status": "watched", "commenter_id": a.CommenterID})
	}
}

func unwatchCommenterMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *commenterIDArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *commenterIDArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		cid, err := parseUUID(a.CommenterID)
		if err != nil {
			return nil, nil, err
		}
		if dbc == nil {
			return nil, nil, fmt.Errorf("database unavailable")
		}
		_, err = dbc.Exec(ctx, `
DELETE FROM commenter_watchlist
WHERE user_id = $1 AND commenter_id = $2`, tokenFrom(ctx).UserID, cid)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{"status": "unwatched", "commenter_id": a.CommenterID})
	}
}

func dismissOSINTFlagMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *dismissOSINTFlagArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *dismissOSINTFlagArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		fid, err := parseUUID(a.FlagID)
		if err != nil {
			return nil, nil, err
		}
		if dbc == nil {
			return nil, nil, fmt.Errorf("database unavailable")
		}
		tag, err := dbc.Exec(ctx, `
UPDATE osint_flags
SET dismissed_at = NOW(), dismissed_by = $1
WHERE id = $2 AND dismissed_at IS NULL`, tokenFrom(ctx).UserID, fid)
		if err != nil {
			return nil, nil, err
		}
		if tag.RowsAffected() == 0 {
			return nil, nil, fmt.Errorf("flag not found or already dismissed")
		}
		return jsonResult(map[string]any{"status": "dismissed", "flag_id": a.FlagID})
	}
}

func assertCommenterLinkMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *assertCommenterLinkArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *assertCommenterLinkArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		aid, err := parseUUID(a.A)
		if err != nil {
			return nil, nil, err
		}
		bid, err := parseUUID(a.B)
		if err != nil {
			return nil, nil, err
		}
		if aid == bid || bytes.Equal(aid.Bytes[:], bid.Bytes[:]) {
			return nil, nil, fmt.Errorf("a_id and b_id must differ")
		}
		lo, hi := orderCommenterPair(aid, bid)
		if dbc == nil {
			return nil, nil, fmt.Errorf("database unavailable")
		}
		evidence, _ := json.Marshal(map[string]any{"asserted_by": "mcp", "kind": "user"})
		_, err = dbc.Exec(ctx, `
INSERT INTO commenter_links (a_id, b_id, kind, score, evidence, created_by)
VALUES ($1, $2, 'user', 1, $3::jsonb, $4)
ON CONFLICT (a_id, b_id, kind)
DO UPDATE SET
  score = EXCLUDED.score,
  evidence = EXCLUDED.evidence,
  created_by = COALESCE(EXCLUDED.created_by, commenter_links.created_by)`,
			lo, hi, evidence, tokenFrom(ctx).UserID)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{
			"status": "asserted",
			"a_id":   uuidString(lo),
			"b_id":   uuidString(hi),
			"kind":   "user",
			"note":   "Operator assertion only; not automatic identification.",
		})
	}
}

func enqueueCommentClassifyMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *videoIDArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *videoIDArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		id, err := parseUUID(a.VideoID)
		if err != nil {
			return nil, nil, err
		}
		if dbc == nil {
			return nil, nil, fmt.Errorf("database unavailable")
		}
		hash := ""
		_ = dbc.QueryRow(ctx, `
SELECT (COALESCE(MAX(c.updated_at), 'epoch'::timestamptz)::text || COUNT(*)::text)::text
FROM video_comments c WHERE c.video_id = $1`, id).Scan(&hash)
		if err := enqueueOSINTMLJob(ctx, dbc, id, "comment_classify", hash); err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{"status": "queued", "video_id": a.VideoID, "kind": "comment_classify", "transcript_hash": hash})
	}
}

type indexXRepliesArgs struct {
	URL     string `json:"url" jsonschema:"X/Twitter status URL"`
	VideoID string `json:"video_id,omitempty" jsonschema:"Optional archived video UUID; otherwise looked up by URL"`
}

func indexXRepliesMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *indexXRepliesArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *indexXRepliesArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		if dbc == nil {
			return nil, nil, fmt.Errorf("database unavailable")
		}
		normalized, canon, err := videoid.NormalizeSourceURL(a.URL)
		if err != nil {
			return nil, nil, err
		}
		if canon != "x.com" || !strings.Contains(normalized, "/status/") {
			return nil, nil, fmt.Errorf("not an X status URL")
		}
		q := dbc.Queries(ctx)
		var videoID pgtype.UUID
		if strings.TrimSpace(a.VideoID) != "" {
			videoID, err = parseUUID(a.VideoID)
			if err != nil {
				return nil, nil, err
			}
		} else {
			video, verr := q.SelectVideoBySrc(ctx, &db.SelectVideoBySrcParams{Src: normalized, TenantID: db.OSSTenant()})
			if verr != nil {
				return nil, nil, fmt.Errorf("archive that X post first, then index replies")
			}
			videoID = video.ID
		}
		if err := comments.IndexXReplies(ctx, q, ytdlp.New(), videoID, normalized); err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{"status": "indexed", "url": normalized, "video_id": uuidString(videoID)})
	}
}

func getChannelCommentEngagementMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *channelArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *channelArgs) (*mcpsdk.CallToolResult, any, error) {
		if dbc == nil {
			return nil, nil, fmt.Errorf("database unavailable")
		}
		if a == nil || strings.TrimSpace(a.ID) == "" {
			return nil, nil, fmt.Errorf("channel id or uploader required")
		}
		resolved, err := lookupChannel(ctx, dbc.Queries(ctx), a.ID)
		if err != nil {
			return nil, nil, err
		}
		var channelID pgtype.UUID
		if resolved.Row != nil {
			channelID = resolved.Row.ID
		}
		eng, err := osint.LoadChannelEngagement(ctx, dbc.Pool, resolved.uploader(), channelID)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{
			"channel":            resolved.uploader(),
			"web_path":           channelWebPath(resolved.uploader()),
			"comment_engagement": eng,
			"caveat":             "Sock flags are observations, not proof. Organic excludes campaign members and open sock_suggest flags.",
		})
	}
}

func enqueueSpeechToneMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *videoIDArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *videoIDArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		id, err := parseUUID(a.VideoID)
		if err != nil {
			return nil, nil, err
		}
		if dbc == nil {
			return nil, nil, fmt.Errorf("database unavailable")
		}
		if err := enqueueOSINTMLJob(ctx, dbc, id, "speech_tone", ""); err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{"status": "queued", "video_id": a.VideoID, "kind": "speech_tone"})
	}
}

func enqueueOSINTMLJob(ctx context.Context, _ *db.DatabaseConnection, videoID pgtype.UUID, kind, transcriptHash string) error {
	return plugin.Enqueue(ctx, plugin.Job{
		VideoID:        uuid.UUID(videoID.Bytes).String(),
		Kind:           kind,
		Priority:       textcls.Priority,
		TranscriptHash: transcriptHash,
		PromptVersion:  textcls.PromptVersion,
	})
}

func queryOSINTFlags(ctx context.Context, dbc *db.DatabaseConnection, kind string, openOnly bool, limit, offset int32, commenterID *pgtype.UUID) ([]map[string]any, error) {
	q := `
SELECT id, kind, commenter_id, video_id, campaign_id, score, evidence, created_at, dismissed_at, dismissed_by
FROM osint_flags
WHERE ($1::text = '' OR kind = $1)
  AND ($2::bool = false OR dismissed_at IS NULL)`
	args := []any{kind, openOnly}
	argn := 3
	if commenterID != nil {
		q += fmt.Sprintf(` AND commenter_id = $%d`, argn)
		args = append(args, *commenterID)
		argn++
	}
	q += fmt.Sprintf(`
ORDER BY created_at DESC
LIMIT $%d OFFSET $%d`, argn, argn+1)
	args = append(args, limit, offset)
	rows, err := dbc.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
	for rows.Next() {
		var (
			id, commenter, video, campaign, dismissedBy pgtype.UUID
			flagKind                                    string
			score                                       float64
			evidence                                    []byte
			createdAt                                   time.Time
			dismissedAt                                 *time.Time
		)
		if err := rows.Scan(&id, &flagKind, &commenter, &video, &campaign, &score, &evidence, &createdAt, &dismissedAt, &dismissedBy); err != nil {
			return nil, err
		}
		item := map[string]any{
			"id":         uuidString(id),
			"kind":       flagKind,
			"score":      score,
			"created_at": createdAt,
		}
		if commenter.Valid {
			item["commenter_id"] = uuidString(commenter)
		}
		if video.Valid {
			vid := uuidString(video)
			item["video_id"] = vid
			item["uri"] = "rewind://video/" + vid
			item["web_path"] = "/videos/" + vid
		}
		if campaign.Valid {
			item["campaign_id"] = uuidString(campaign)
		}
		if dismissedAt != nil {
			item["dismissed_at"] = *dismissedAt
		}
		if dismissedBy.Valid {
			item["dismissed_by"] = uuidString(dismissedBy)
		}
		if len(evidence) > 0 {
			var ev any
			if json.Unmarshal(evidence, &ev) == nil {
				item["evidence"] = ev
			}
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func queryCommenterCampaigns(ctx context.Context, dbc *db.DatabaseConnection, commenterID pgtype.UUID) ([]map[string]any, error) {
	rows, err := dbc.Query(ctx, `
SELECT DISTINCT camp.id, camp.kind, camp.simhash, camp.normalized_text, camp.first_seen, camp.last_seen,
       camp.comment_count, camp.commenter_count, camp.video_count, camp.evidence, camp.created_at, camp.updated_at
FROM campaigns camp
JOIN campaign_members cm ON cm.campaign_id = camp.id
JOIN video_comments vc ON vc.id = cm.comment_id
WHERE vc.commenter_id = $1
ORDER BY camp.last_seen DESC`, commenterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
	for rows.Next() {
		item, err := scanCampaign(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func queryStyleNeighbors(ctx context.Context, dbc *db.DatabaseConnection, commenterID pgtype.UUID) ([]map[string]any, error) {
	rows, err := dbc.Query(ctx, `
SELECT a_id, b_id, kind, score, evidence, created_at
FROM commenter_links
WHERE (a_id = $1 OR b_id = $1) AND kind = 'style'
ORDER BY score DESC, created_at DESC
LIMIT 20`, commenterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
	for rows.Next() {
		var (
			aID, bID  pgtype.UUID
			kind      string
			score     float64
			evidence  []byte
			createdAt time.Time
		)
		if err := rows.Scan(&aID, &bID, &kind, &score, &evidence, &createdAt); err != nil {
			return nil, err
		}
		neighbor := bID
		if bytes.Equal(bID.Bytes[:], commenterID.Bytes[:]) {
			neighbor = aID
		}
		item := map[string]any{
			"commenter_id": uuidString(neighbor),
			"kind":         kind,
			"score":        score,
			"created_at":   createdAt,
			"hypothesis":   true,
		}
		if len(evidence) > 0 {
			var ev any
			if json.Unmarshal(evidence, &ev) == nil {
				item["evidence"] = ev
			}
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func queryCommenterEvidence(ctx context.Context, dbc *db.DatabaseConnection, commenterID pgtype.UUID, limit int32) ([]map[string]any, error) {
	rows, err := dbc.Query(ctx, `
SELECT c.id, c.video_id, c.author, c.text, v.title
FROM video_comments c
JOIN videos v ON v.id = c.video_id
WHERE c.commenter_id = $1
ORDER BY c.published_at DESC NULLS LAST, c.created_at DESC
LIMIT $2`, commenterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
	for rows.Next() {
		var (
			cid, vid     pgtype.UUID
			author, text *string
			title        string
		)
		if err := rows.Scan(&cid, &vid, &author, &text, &title); err != nil {
			return nil, err
		}
		sid := uuidString(vid)
		item := map[string]any{
			"comment_id": uuidString(cid),
			"video_id":   sid,
			"uri":        "rewind://video/" + sid,
			"web_path":   "/videos/" + sid,
			"title":      title,
		}
		if author != nil {
			item["author"] = *author
		}
		if text != nil {
			item["text"] = *text
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

type campaignScanner interface {
	Scan(dest ...any) error
}

func scanCampaign(row campaignScanner) (map[string]any, error) {
	var (
		id                                       pgtype.UUID
		kind, normalized                         string
		simhash                                  *int64
		firstSeen, lastSeen, created, upd        time.Time
		commentCount, commenterCount, videoCount int64
		evidence                                 []byte
	)
	if err := row.Scan(&id, &kind, &simhash, &normalized, &firstSeen, &lastSeen, &commentCount, &commenterCount, &videoCount, &evidence, &created, &upd); err != nil {
		return nil, err
	}
	item := map[string]any{
		"id":              uuidString(id),
		"kind":            kind,
		"normalized_text": normalized,
		"first_seen":      firstSeen,
		"last_seen":       lastSeen,
		"comment_count":   commentCount,
		"commenter_count": commenterCount,
		"video_count":     videoCount,
		"created_at":      created,
		"updated_at":      upd,
	}
	if simhash != nil {
		item["simhash"] = *simhash
	}
	if len(evidence) > 0 {
		var ev any
		if json.Unmarshal(evidence, &ev) == nil {
			item["evidence"] = ev
		}
	}
	return item, nil
}

func orderCommenterPair(a, b pgtype.UUID) (pgtype.UUID, pgtype.UUID) {
	au := uuid.UUID(a.Bytes)
	bu := uuid.UUID(b.Bytes)
	if bytes.Compare(au[:], bu[:]) <= 0 {
		return a, b
	}
	return b, a
}
