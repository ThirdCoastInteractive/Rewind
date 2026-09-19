package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/frames"
	"thirdcoast.systems/rewind/internal/jsnum"
	"thirdcoast.systems/rewind/internal/stitch"
	"thirdcoast.systems/rewind/pkg/captions"
)

const teaserMaxSeconds = 180.0

type teaserSuggestArgs struct {
	VideoID        string     `json:"video_id,omitempty"`
	ChannelID      string     `json:"channel_id,omitempty"`
	CreatorID      string     `json:"creator_id,omitempty"`
	Query          string     `json:"query,omitempty"`
	Queries        []string   `json:"queries,omitempty"`
	QueryGroups    [][]string `json:"query_groups,omitempty"`
	Limit          int32      `json:"limit,omitempty"`
	MaxDuration    jsnum.F    `json:"max_duration_seconds,omitempty"`
	ContextSeconds float64    `json:"context_seconds,omitempty"`
	Offset         int32      `json:"offset,omitempty"`
	PassageOffset  int        `json:"passage_offset,omitempty"`
}

type teaserRange struct {
	VideoID string               `json:"video_id"`
	Start   jsnum.F              `json:"start"`
	End     jsnum.F              `json:"end"`
	Title   string               `json:"title,omitempty"`
	Layout  *stitch.TeaserLayout `json:"layout,omitempty"`
}

type createTeaserArgs struct {
	Title          string               `json:"title,omitempty"`
	OperationKey   string               `json:"operation_key,omitempty"`
	IdempotencyKey string               `json:"idempotency_key,omitempty"`
	Width          int                  `json:"width,omitempty"`
	Height         int                  `json:"height,omitempty"`
	Segments       []teaserRange        `json:"segments,omitempty"`
	SourceRanges   []teaserRange        `json:"source_ranges,omitempty"`
	Layout         *stitch.TeaserLayout `json:"layout,omitempty"`
}

type setTeaserLayoutArgs struct {
	ProjectID        string              `json:"project_id"`
	ExpectedRevision *int64              `json:"expected_revision"`
	OperationKey     string              `json:"operation_key"`
	TargetID         string              `json:"target_id,omitempty"`
	SegmentID        string              `json:"segment_id,omitempty"`
	Layout           stitch.TeaserLayout `json:"layout,omitempty"`
	Mode             string              `json:"mode,omitempty"`
	Crops            []stitch.TeaserCrop `json:"crops,omitempty"`
	Width            int                 `json:"width,omitempty"`
	Height           int                 `json:"height,omitempty"`
}

type checkTeaserArgs struct {
	ProjectID           string `json:"project_id"`
	QueuePreview        bool   `json:"queue_preview,omitempty"`
	OperationKey        string `json:"operation_key,omitempty"`
	PreviewOperationKey string `json:"preview_operation_key,omitempty"`
}

// registerTeaserTools registers short-form search, creation, layout, and QA tools.
// The server owns registration order; this function intentionally does not edit server.go.
func registerTeaserTools(srv *mcpsdk.Server, dbc *db.DatabaseConnection) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "get_shortform_workflow", Description: "Explain the evidence-first workflow for finding, creating, laying out, captioning, and checking a short-form teaser."}, func(context.Context, *mcpsdk.CallToolRequest, *struct{}) (*mcpsdk.CallToolResult, any, error) {
		return jsonResult(map[string]any{"workflow": shortformWorkflow})
	})
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "suggest_teasers", Description: "Find bounded short-form teaser candidates from indexed transcript evidence. Results include spoken ranges, evidence, and reasons; the search is not exhaustive."}, suggestTeasers(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "create_teaser", Description: "Create an owned editable canonical Stitch teaser from selected source ranges. The operation key makes retries safe and does not render media."}, createTeaser(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "set_teaser_layout", Description: "Apply a revisioned teaser canvas or segment layout using normalized crops. Requires an expected revision and operation key."}, setTeaserLayout(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "check_teaser", Description: "Check owned teaser structure, timing, caption presence, and source media readiness. Structural checks do not claim visual or audio analysis."}, checkTeaser(dbc))
}

const shortformWorkflow = `Find a short-form teaser with evidence first. For known videos, inspect get_context_windows first: nested shorts contain generated spoken beats and hooks. Prefer those short boundaries over whole parent chapters; one short per beat. If nested shorts are missing, enqueue_context_windows can generate them, or use transcript-backed suggest_teasers. Never invent timestamps. Call suggest_teasers with a known video_id, channel_id, creator_id, or a topic query. Review each candidate's evidence and spoken segment bounds; transcript coverage is indexed evidence and may be incomplete. Select source ranges, then call create_teaser with a stable operation_key. Use set_teaser_layout with the returned revision for a 1080x1920 canvas or per-segment crop. Call style_teaser_captions when captions are needed, then check_teaser. check_teaser reports structural, caption, media, and timing issues only; use stitch_preview or stitch_frame for visual review and inspect audio through the rendered preview. Follow pagination when suggest_teasers reports more candidates.`

func suggestTeasers(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *teaserSuggestArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *teaserSuggestArgs) (*mcpsdk.CallToolResult, any, error) {
		if strings.TrimSpace(a.Query) == "" && len(a.Queries) == 0 && len(a.QueryGroups) == 0 && strings.TrimSpace(a.VideoID) == "" && strings.TrimSpace(a.ChannelID) == "" && strings.TrimSpace(a.CreatorID) == "" {
			return nil, nil, fmt.Errorf("provide a topic query or video_id, channel_id, or creator_id scope")
		}
		if a.Limit <= 0 || a.Limit > 25 {
			a.Limit = 10
		}
		maxDuration := float64(a.MaxDuration)
		if maxDuration <= 0 {
			maxDuration = 60
		}
		if maxDuration > teaserMaxSeconds {
			return nil, nil, fmt.Errorf("max_duration_seconds must be at most %.0f", teaserMaxSeconds)
		}
		if strings.TrimSpace(a.Query) == "" && len(a.Queries) == 0 && len(a.QueryGroups) == 0 {
			if shorts, err := suggestTeasersFromWindows(ctx, dbc, a.VideoID, maxDuration, a.Limit, a.Offset); err != nil {
				return nil, nil, err
			} else if len(shorts) > 0 {
				return jsonResult(map[string]any{"candidates": shorts, "suggestions": shorts, "returned_match_count": len(shorts), "next_offset": a.Offset + int32(len(shorts)), "next_passage_offset": 0, "has_more_candidates": len(shorts) == int(a.Limit), "max_duration_seconds": maxDuration, "coverage": "nested context-window shorts from the current generation; cut these bounds and do not widen to the parent chapter"})
			}
			return scopeOnlyTeasers(ctx, dbc, a, maxDuration)
		}
		result, _, err := searchTranscriptPage(dbc, true)(ctx, nil, &transcriptSearchV2Args{VideoID: a.VideoID, ChannelID: a.ChannelID, CreatorID: a.CreatorID, Query: a.Query, Queries: a.Queries, QueryGroups: a.QueryGroups, Limit: a.Limit, ContextSeconds: a.ContextSeconds, Offset: a.Offset, PassageOffset: a.PassageOffset})
		if err != nil {
			return nil, nil, err
		}
		var envelope struct {
			Passages          []map[string]any `json:"passages"`
			NextOffset        int32            `json:"next_offset"`
			NextPassageOffset int              `json:"next_passage_offset"`
			HasMore           bool             `json:"has_more_candidates"`
		}
		if len(result.Content) == 0 {
			return nil, nil, fmt.Errorf("candidate search returned no result")
		}
		text, ok := result.Content[0].(*mcpsdk.TextContent)
		if !ok || json.Unmarshal([]byte(text.Text), &envelope) != nil {
			return nil, nil, fmt.Errorf("candidate search returned malformed result")
		}
		out := make([]map[string]any, 0, len(envelope.Passages))
		for _, p := range envelope.Passages {
			segment, ok := p["segment"].(map[string]any)
			if !ok {
				continue
			}
			start, sok := numberField(segment, "start")
			end, eok := numberField(segment, "end")
			if !sok || !eok || end <= start || end-start > maxDuration {
				continue
			}
			p["reason"] = "indexed transcript evidence supports a spoken beat; verify relevance and speaker attribution before creating"
			out = append(out, p)
		}
		return jsonResult(map[string]any{"candidates": out, "suggestions": out, "returned_match_count": len(out), "next_offset": envelope.NextOffset, "next_passage_offset": envelope.NextPassageOffset, "has_more_candidates": envelope.HasMore, "max_duration_seconds": maxDuration, "coverage": "available indexed transcripts only; missing or partial transcripts limit recall"})
	}
}

// scopeOnlyTeasers selects a few deterministic transcript beats when an agent
// names an episode or scope but does not provide a topic. It exposes the
// actual cue text and labels the heuristic so it is never mistaken for an
// editorial or visual ranking.
func suggestTeasersFromWindows(ctx context.Context, dbc *db.DatabaseConnection, videoID string, maxDuration float64, limit, offset int32) ([]map[string]any, error) {
	if dbc == nil || strings.TrimSpace(videoID) == "" {
		return nil, nil
	}
	id, err := parseUUID(videoID)
	if err != nil {
		return nil, err
	}
	rows, err := dbc.Queries(ctx).ListContextWindowsForVideo(ctx, &db.ListContextWindowsForVideoParams{VideoID: id})
	if err != nil {
		return nil, err
	}
	var all []map[string]any
	for _, w := range nestContextWindows(rows) {
		for _, sh := range w.Shorts {
			if sh == nil || sh.EndTs <= sh.StartTs || sh.EndTs-sh.StartTs > maxDuration {
				continue
			}
			all = append(all, teaserCandidateFromShort(w, sh))
		}
	}
	if offset < 0 {
		offset = 0
	}
	if int(offset) >= len(all) {
		return nil, nil
	}
	end := int(offset) + int(limit)
	if limit <= 0 || end > len(all) {
		end = len(all)
	}
	return all[int(offset):end], nil
}

func teaserCandidateFromShort(parent nestedContextWindow, sh *db.ListContextWindowsForVideoRow) map[string]any {
	vid := uuidString(sh.VideoID)
	evidence := strings.TrimSpace(sh.Hook)
	if evidence == "" {
		evidence = strings.TrimSpace(sh.Summary)
	}
	if evidence == "" {
		evidence = strings.TrimSpace(sh.Title)
	}
	parentTitle := ""
	if parent.ListContextWindowsForVideoRow != nil {
		parentTitle = parent.Title
	}
	title := strings.TrimSpace(sh.Title)
	if parentTitle != "" && title != "" {
		title = parentTitle + " · " + title
	}
	return map[string]any{
		"video_id":          vid,
		"title":             title,
		"timestamp":         sh.StartTs,
		"evidence":          evidence,
		"hook_excerpt":      strings.TrimSpace(sh.Hook),
		"kind":              "short",
		"parent_window_id":  uuidString(parent.ID),
		"context_window_id": uuidString(sh.ID),
		"segment":           planSegmentInput{VideoID: vid, Start: sh.StartTs, End: sh.EndTs, ContextWindowID: uuidString(sh.ID), MatchEvidence: map[string]any{"text": evidence, "timestamp": sh.StartTs, "kind": "short"}},
		"reason":            "nested context-window short generated with the parent chapter; cut these bounds, do not widen to the parent window",
		"web_path":          fmt.Sprintf("/videos/%s?t=%.3f", vid, sh.StartTs),
	}
}

func scopeOnlyTeasers(ctx context.Context, dbc *db.DatabaseConnection, a *teaserSuggestArgs, maxDuration float64) (*mcpsdk.CallToolResult, any, error) {
	if dbc == nil {
		return nil, nil, fmt.Errorf("a database is required for scope-only transcript suggestions")
	}
	videoID, err := optionalUUID(a.VideoID)
	if err != nil {
		return nil, nil, err
	}
	channelID, err := optionalUUID(a.ChannelID)
	if err != nil {
		return nil, nil, err
	}
	creatorID, err := optionalUUID(a.CreatorID)
	if err != nil {
		return nil, nil, err
	}
	rows, err := dbc.Query(ctx, `SELECT vt.video_id,v.title,v.uploader,v.media,v.duration_seconds,vt.lang::text,vt.cues FROM video_transcripts vt JOIN videos v ON v.id=vt.video_id LEFT JOIN channels ch ON ch.id=v.channel_row_id WHERE ($1::uuid IS NULL OR v.id=$1) AND ($2::uuid IS NULL OR ch.id=$2) AND ($3::uuid IS NULL OR ch.creator_id=$3) ORDER BY vt.video_id,vt.lang::text LIMIT $4 OFFSET $5`, videoID, channelID, creatorID, a.Limit, a.Offset)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	type scopeRow struct {
		videoID                      pgtype.UUID
		title, uploader, media, lang string
		duration                     *float64
		cues                         []byte
	}
	var sourceRows []scopeRow
	for rows.Next() {
		var r scopeRow
		if err := rows.Scan(&r.videoID, &r.title, &r.uploader, &r.media, &r.duration, &r.lang, &r.cues); err != nil {
			return nil, nil, err
		}
		sourceRows = append(sourceRows, r)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	out := make([]map[string]any, 0, a.Limit)
	nextOffset := a.Offset
	nextPassageOffset := 0
	stopped := false
	passageOffset := a.PassageOffset
	for rowIndex, row := range sourceRows {
		if len(out) >= int(a.Limit) {
			stopped = true
			nextOffset = a.Offset + int32(rowIndex)
			nextPassageOffset = 0
			break
		}
		cues, err := decodeTranscriptCues(row.cues)
		if err != nil {
			return nil, nil, fmt.Errorf("video %s transcript: %w", uuidString(row.videoID), err)
		}
		if len(cues) == 0 {
			continue
		}
		indices := scopeBeatIndices(cues)
		for candidateIndex, i := range indices {
			if len(out) >= int(a.Limit) {
				stopped = true
				nextOffset = a.Offset + int32(rowIndex)
				nextPassageOffset = candidateIndex
				break
			}
			if passageOffset > 0 {
				passageOffset--
				continue
			}
			cue := cues[i]
			if strings.TrimSpace(cue.Text) == "" || cue.End <= cue.Start {
				continue
			}
			start, boundedEnd := scopeBeatBounds(cues, i, maxDuration)
			if row.duration != nil && *row.duration > 0 && boundedEnd > *row.duration {
				boundedEnd = *row.duration
			}
			if boundedEnd <= start {
				continue
			}
			evidence := strings.TrimSpace(cue.Text)
			segment := planSegmentInput{VideoID: uuidString(row.videoID), Start: start, End: boundedEnd, MatchEvidence: map[string]any{"text": evidence, "timestamp": cue.Start, "language": row.lang}}
			setup, payoff := "", ""
			if i > 0 {
				setup = strings.TrimSpace(cues[i-1].Text)
			}
			if i+1 < len(cues) {
				payoff = strings.TrimSpace(cues[i+1].Text)
			}
			out = append(out, map[string]any{"video_id": segment.VideoID, "title": row.title, "uploader": row.uploader, "language": row.lang, "timestamp": cue.Start, "evidence": evidence, "context": strings.TrimSpace(strings.Join([]string{setup, evidence, payoff}, " ")), "hook_excerpt": evidence, "setup_excerpt": setup, "setup_context_only": setup != "", "payoff_excerpt": payoff, "payoff_context_only": payoff != "", "segment": segment, "media_status": map[bool]string{true: "catalog-only", false: "playable"}[row.media == "metadata"], "sampled": true, "reason": "scope-only deterministic hook/setup/payoff transcript heuristic; no topic, visual, or audio ranking was requested", "web_path": fmt.Sprintf("/videos/%s?t=%.3f", segment.VideoID, cue.Start)})
		}
		if stopped {
			break
		}
		nextOffset = a.Offset + int32(rowIndex+1)
	}
	return jsonResult(map[string]any{"candidates": out, "suggestions": out, "returned_match_count": len(out), "next_offset": nextOffset, "next_passage_offset": nextPassageOffset, "has_more_candidates": stopped || nextOffset < a.Offset+int32(len(sourceRows)) || len(sourceRows) == int(a.Limit), "max_duration_seconds": maxDuration, "coverage": "available indexed transcripts only; scope-only beats are a deterministic heuristic, not a relevance ranking"})
}

func scopeBeatIndices(cues []captions.Cue) []int {
	if len(cues) == 0 {
		return nil
	}
	indices := []int{0, len(cues) / 3, (2 * len(cues)) / 3}
	out := make([]int, 0, len(indices))
	seen := map[int]bool{}
	for _, i := range indices {
		if i >= 0 && i < len(cues) && !seen[i] {
			seen[i] = true
			out = append(out, i)
		}
	}
	return out
}

func scopeBeatBounds(cues []captions.Cue, index int, maxDuration float64) (float64, float64) {
	start, end := cues[index].Start, cues[index].End
	if end <= start || end-start > maxDuration {
		return start, start
	}
	for i := index + 1; i < len(cues); i++ {
		if cues[i].End <= cues[i].Start || cues[i].Start-end > 1 || cues[i].End-start > maxDuration {
			break
		}
		end = cues[i].End
	}
	return start, end
}

func createTeaser(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *createTeaserArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *createTeaserArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		tok := tokenFrom(ctx)
		if tok == nil {
			return nil, nil, fmt.Errorf("authentication required")
		}
		key := strings.TrimSpace(a.OperationKey)
		if key == "" {
			key = strings.TrimSpace(a.IdempotencyKey)
		}
		if key == "" {
			return nil, nil, fmt.Errorf("operation_key is required")
		}
		ranges := a.Segments
		if len(ranges) == 0 {
			ranges = a.SourceRanges
		}
		if len(ranges) == 0 || len(ranges) > 100 {
			return nil, nil, fmt.Errorf("segments must contain 1-100 items")
		}
		var total float64
		segments := make([]stitch.Segment, 0, len(ranges))
		for i, r := range ranges {
			start, end := float64(r.Start), float64(r.End)
			if _, err := parseUUID(r.VideoID); err != nil {
				return nil, nil, fmt.Errorf("segment %d: %w", i+1, err)
			}
			if !finiteNumber(start) || !finiteNumber(end) || start < 0 || end <= start || end-start > teaserMaxSeconds {
				return nil, nil, fmt.Errorf("segment %d has invalid or unbounded range", i+1)
			}
			total += end - start
			if total > teaserMaxSeconds {
				return nil, nil, fmt.Errorf("teaser duration must be at most %.0f seconds", teaserMaxSeconds)
			}
			seg := stitch.Segment{ID: fmt.Sprintf("teaser-segment-%d", i+1), Type: "video", VideoID: r.VideoID, StartUS: int64((total - (end - start)) * 1e6), SourceInUS: int64(start * 1e6), DurationUS: int64((end - start) * 1e6), Text: strings.TrimSpace(r.Title)}
			layout := a.Layout
			if r.Layout != nil {
				layout = r.Layout
			}
			if layout == nil {
				layout = stitch.DefaultTeaserLayout()
			}
			if err := layout.Validate(); err != nil {
				return nil, nil, fmt.Errorf("segment %d layout: %w", i+1, err)
			}
			layoutCopy := *layout
			layoutCopy.Crops = append([]stitch.TeaserCrop(nil), layout.Crops...)
			seg.Layout = &layoutCopy
			segments = append(segments, seg)
		}
		result, err := stitch.NewStore(dbc).CreateTeaser(ctx, stitch.TeaserCreateInput{Owner: tok.UserID, OperationKey: key, Title: a.Title, Width: a.Width, Height: a.Height, Segments: segments, Actor: stitch.Actor{Kind: "agent", ID: ActorFrom(ctx).ID, Name: ActorFrom(ctx).ClientName}})
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{"project_id": uuidString(result.Snapshot.ID), "revision": result.Snapshot.Revision, "title": result.Snapshot.Document.Title, "width": result.Snapshot.Document.Width, "height": result.Snapshot.Document.Height, "status": "editable", "created": result.Created, "operation_key": key, "web_path": "/stitch/" + uuidString(result.Snapshot.ID), "document": result.Snapshot.Document})
	}
}

func setTeaserLayout(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *setTeaserLayoutArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *setTeaserLayoutArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		if a.ExpectedRevision == nil {
			return nil, nil, fmt.Errorf("expected_revision is required")
		}
		if strings.TrimSpace(a.OperationKey) == "" {
			return nil, nil, fmt.Errorf("operation_key is required")
		}
		tok := tokenFrom(ctx)
		if tok == nil {
			return nil, nil, fmt.Errorf("authentication required")
		}
		id, err := parseUUID(a.ProjectID)
		if err != nil {
			return nil, nil, err
		}
		ops := make([]stitch.Operation, 0, 2)
		layout := a.Layout
		if layout.Mode == "" {
			layout.Mode = a.Mode
		}
		if len(layout.Crops) == 0 {
			layout.Crops = a.Crops
		}
		if a.TargetID == "" {
			a.TargetID = a.SegmentID
		}
		if layout.Mode != "" || len(layout.Crops) > 0 {
			if err := validateTeaserLayout(layout); err != nil {
				return nil, nil, err
			}
			if a.TargetID == "" {
				return nil, nil, fmt.Errorf("target_id is required for a segment layout")
			}
			ops = append(ops, stitch.Operation{Type: "set_segment_layout", TargetID: a.TargetID, Layout: &layout})
		}
		if a.Width != 0 || a.Height != 0 {
			if a.Width <= 0 || a.Height <= 0 || a.Width > 16384 || a.Height > 16384 || a.Width%2 != 0 || a.Height%2 != 0 {
				return nil, nil, fmt.Errorf("canvas width and height must be positive even values no larger than 16384")
			}
			ops = append(ops, stitch.Operation{Type: "set_canvas", Width: a.Width, Height: a.Height})
		}
		if len(ops) == 0 {
			return nil, nil, fmt.Errorf("provide layout or canvas dimensions")
		}
		ac := ActorFrom(ctx)
		result, err := stitch.NewStore(dbc).Commit(ctx, tok.UserID, id, *a.ExpectedRevision, strings.TrimSpace(a.OperationKey), stitch.Actor{Kind: string(ac.Kind), ID: ac.ID, Name: ac.ClientName}, "set teaser layout", ops)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(result)
	}
}

func validateTeaserLayout(l stitch.TeaserLayout) error {
	return l.Validate()
}

func checkTeaser(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *checkTeaserArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *checkTeaserArgs) (*mcpsdk.CallToolResult, any, error) {
		tok := tokenFrom(ctx)
		if tok == nil {
			return nil, nil, fmt.Errorf("authentication required")
		}
		id, err := parseUUID(a.ProjectID)
		if err != nil {
			return nil, nil, err
		}
		snap, err := stitch.NewStore(dbc).Get(ctx, tok.UserID, id)
		if err != nil {
			return nil, nil, err
		}
		issues := []map[string]any{}
		if err := stitch.Validate(snap.Document); err != nil {
			issues = append(issues, map[string]any{"kind": "timing", "severity": "error", "message": "canonical document validation failed", "detail": err.Error(), "action": "fix the indicated segment or caption ranges with a revisioned Stitch edit"})
		}
		duration := documentDurationSeconds(snap.Document)
		if duration <= 0 {
			issues = append(issues, map[string]any{"kind": "timing", "severity": "error", "message": "teaser has no positive timeline duration"})
		} else if duration > teaserMaxSeconds {
			issues = append(issues, map[string]any{"kind": "timing", "severity": "error", "message": fmt.Sprintf("teaser duration exceeds Rewind's %.0f-second work limit", teaserMaxSeconds), "duration_seconds": duration, "action": "trim or remove segments"})
		}
		if snap.Document.Width != 1080 || snap.Document.Height != 1920 {
			issues = append(issues, map[string]any{"kind": "canvas", "severity": "warning", "message": "teaser canvas is not the default 1080x1920 vertical size", "action": "use set_teaser_layout with width=1080 and height=1920 if vertical output is intended"})
		}
		videoIDs := make([]pgtype.UUID, 0)
		for _, s := range snap.Document.Segments {
			if s.VideoID != "" {
				if v, e := parseUUID(s.VideoID); e == nil {
					videoIDs = append(videoIDs, v)
				} else {
					issues = append(issues, map[string]any{"kind": "media", "severity": "error", "segment_id": s.ID, "message": "segment has an invalid source video id"})
				}
			}
		}
		if len(snap.Document.Captions) == 0 {
			issues = append(issues, map[string]any{"kind": "captions", "severity": "warning", "message": "no captions are attached to this teaser", "action": "call style_teaser_captions after importing or creating captions"})
		}
		segmentsByID := make(map[string]stitch.Segment, len(snap.Document.Segments))
		for _, s := range snap.Document.Segments {
			segmentsByID[s.ID] = s
		}
		for _, c := range snap.Document.Captions {
			if s, ok := segmentsByID[c.SegmentID]; ok {
				if c.StartUS < s.StartUS || c.EndUS > s.StartUS+s.DurationUS {
					issues = append(issues, map[string]any{"kind": "captions", "severity": "error", "caption_id": c.ID, "message": "caption timing extends outside its segment", "action": "trim or realign the caption to the segment timeline"})
				}
			}
			if c.Style.X < 0 || c.Style.Y < 0 || c.Style.X >= float64(snap.Document.Width) || c.Style.Y >= float64(snap.Document.Height) {
				if c.Style.X != 0 || c.Style.Y != 0 {
					issues = append(issues, map[string]any{"kind": "captions", "severity": "error", "caption_id": c.ID, "message": "caption placement is outside the canvas", "action": "move caption x/y inside the canvas"})
				}
			}
			if c.Style.FontSize > 0 {
				maxLineRunes := 0
				for _, line := range strings.Split(strings.ReplaceAll(c.Text, "\r\n", "\n"), "\n") {
					if n := utf8.RuneCountInString(line); n > maxLineRunes {
						maxLineRunes = n
					}
				}
				estimatedWidth := float64(maxLineRunes) * c.Style.FontSize * 0.6
				lineHeight := c.Style.FontSize * 1.2
				lineCount := len(strings.Split(strings.ReplaceAll(c.Text, "\r\n", "\n"), "\n"))
				if lineCount < 1 {
					lineCount = 1
				}
				estimatedHeight := lineHeight * float64(lineCount)
				centerX, centerY := c.Style.X, c.Style.Y
				if centerX == 0 {
					centerX = float64(snap.Document.Width) / 2
				}
				if centerY == 0 {
					centerY = float64(snap.Document.Height) - lineHeight
				}
				if centerX-estimatedWidth/2 < 0 || centerX+estimatedWidth/2 > float64(snap.Document.Width) || centerY-estimatedHeight/2 < 0 || centerY+estimatedHeight/2 > float64(snap.Document.Height) {
					issues = append(issues, map[string]any{"kind": "captions", "severity": "warning", "caption_id": c.ID, "message": "caption text may overflow the canvas at its configured font size", "action": "shorten the line, reduce font size, or move x"})
				}
			}
		}
		unavailable := map[pgtype.UUID]bool{}
		if len(videoIDs) > 0 && dbc != nil {
			rows, e := dbc.Query(ctx, `SELECT id, media, duration_seconds, video_path FROM videos WHERE id=ANY($1::uuid[])`, videoIDs)
			if e != nil {
				return nil, nil, e
			}
			seen := map[pgtype.UUID]bool{}
			for rows.Next() {
				var vid pgtype.UUID
				var media string
				var sourceDuration *float64
				var videoPath *string
				if e := rows.Scan(&vid, &media, &sourceDuration, &videoPath); e != nil {
					rows.Close()
					return nil, nil, e
				}
				seen[vid] = true
				if media == "metadata" {
					unavailable[vid] = true
					issues = append(issues, map[string]any{"kind": "media", "severity": "error", "video_id": uuidString(vid), "message": "source is catalog-only and has no playable media", "action": "enqueue_download before preview or export"})
				}
				storedPath := ""
				if videoPath != nil {
					storedPath = strings.TrimSpace(*videoPath)
				}
				dur := 0.0
				if sourceDuration != nil {
					dur = *sourceDuration
				}
				asset, resolveErr := frames.Resolve(uuidString(vid), storedPath, dur)
				if resolveErr != nil || asset.File == "" {
					unavailable[vid] = true
					issues = append(issues, map[string]any{"kind": "media", "severity": "error", "video_id": uuidString(vid), "message": "source has no local media path", "action": "enqueue_download before preview or export"})
				}
				for _, s := range snap.Document.Segments {
					if s.VideoID == uuidString(vid) && sourceDuration != nil && *sourceDuration > 0 && float64(s.SourceInUS+s.DurationUS)/1e6 > *sourceDuration+0.001 {
						issues = append(issues, map[string]any{"kind": "timing", "severity": "error", "segment_id": s.ID, "message": "segment source range exceeds video duration", "source_duration_seconds": *sourceDuration})
					}
				}
			}
			if e := rows.Err(); e != nil {
				rows.Close()
				return nil, nil, e
			}
			rows.Close()
			for _, vid := range videoIDs {
				if !seen[vid] {
					unavailable[vid] = true
					issues = append(issues, map[string]any{"kind": "media", "severity": "error", "video_id": uuidString(vid), "message": "source video was not found"})
				}
			}
		}
		missing := len(unavailable)
		out := map[string]any{"project_id": a.ProjectID, "revision": snap.Revision, "duration_seconds": documentDurationSeconds(snap.Document), "issues": issues, "source_media_missing": missing, "structural_checks": []string{"document validation", "canvas dimensions", "segment timing", "caption presence", "source media readiness"}, "analysis_limitations": []string{"No visual speaker detection was performed", "No audio quality or loudness analysis was performed", "Use stitch_preview or stitch_frame and inspect the rendered media for those checks"}, "next_tools": []string{"stitch_preview", "stitch_frame"}}
		frameTime := int64(duration * 500000)
		out["next_calls"] = []map[string]any{
			{"tool": "stitch_preview", "arguments": map[string]any{"project_id": a.ProjectID, "revision": snap.Revision, "operation_key": fmt.Sprintf("check-preview:%s:%d", a.ProjectID, snap.Revision), "format": "mp4", "quality": "high", "caption_mode": "burn", "scope": "all"}},
			{"tool": "stitch_frame", "arguments": map[string]any{"project_id": a.ProjectID, "revision": snap.Revision, "operation_key": fmt.Sprintf("check-frame:%s:%d", a.ProjectID, snap.Revision), "frame_time_us": frameTime, "format": "mp4", "quality": "high", "caption_mode": "burn", "scope": "all"}},
		}
		if a.QueuePreview {
			if err := requireWrite(ctx); err != nil {
				return nil, nil, err
			}
			key := strings.TrimSpace(a.PreviewOperationKey)
			if key == "" {
				key = strings.TrimSpace(a.OperationKey)
			}
			if key == "" {
				return nil, nil, fmt.Errorf("preview operation_key is required when queue_preview is true")
			}
			job, e := stitch.NewStore(dbc).QueuePreview(ctx, tok.UserID, id, snap.Revision, key, stitch.RenderOptions{Format: "mp4", Quality: "high", CaptionMode: "burn", Scope: "all"})
			if e != nil {
				return nil, nil, e
			}
			out["preview_job"] = job
		}
		return jsonResult(out)
	}
}

func numberField(m map[string]any, key string) (float64, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case json.Number:
		x, e := n.Float64()
		return x, e == nil
	default:
		return 0, false
	}
}
func finiteNumber(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func documentDurationSeconds(d stitch.Document) float64 {
	var end int64
	for _, s := range d.Segments {
		if s.StartUS+s.DurationUS > end {
			end = s.StartUS + s.DurationUS
		}
	}
	return float64(end) / 1e6
}
