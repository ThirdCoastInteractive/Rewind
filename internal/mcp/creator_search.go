package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"thirdcoast.systems/rewind/internal/catalog"
	"thirdcoast.systems/rewind/internal/compilation"
	"thirdcoast.systems/rewind/internal/contextwindow"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/jsnum"
	"thirdcoast.systems/rewind/internal/search"
	"thirdcoast.systems/rewind/internal/waveform"
	"thirdcoast.systems/rewind/pkg/captions"
)

type transcriptSearchV2Args struct {
	VideoID        string     `json:"video_id,omitempty" jsonschema:"Restrict to one known video UUID, including guest appearances; may combine with channel or creator filters"`
	QueryGroups    [][]string `json:"query_groups,omitempty" jsonschema:"Required topic groups; alternatives within each group are ANY, groups are ALL within context_seconds. Example: [[Callen,Callan],[Tesla,test lid]]. Exclusive with query/queries."`
	MatchScope     string     `json:"match_scope,omitempty" jsonschema:"cue (default) or window. Window matches terms across nearby transcript cues within context_seconds."`
	PassageOffset  int        `json:"passage_offset,omitempty" jsonschema:"Within-video continuation from next_passage_offset"`
	Offset         int32      `json:"offset,omitempty" jsonschema:"Video candidate offset from next_offset"`
	Query          string     `json:"query,omitempty" jsonschema:"Words or quoted phrase; words within a query must all match. Use queries for alternatives, not OR syntax."`
	Queries        []string   `json:"queries,omitempty" jsonschema:"Up to 8 alternative queries, matched with ANY semantics; e.g. Adam, quoted Adam Sellers, hot tub. Supply query or queries, not both."`
	CreatorID      string     `json:"creator_id,omitempty"`
	ChannelID      string     `json:"channel_id,omitempty"`
	Limit          int32      `json:"limit,omitempty"`
	ContextSeconds float64    `json:"context_seconds,omitempty"`
}

func searchTranscriptsV2(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *transcriptSearchV2Args) (*mcpsdk.CallToolResult, any, error) {
	return searchTranscriptPage(dbc, false)
}

func searchTranscriptPage(dbc *db.DatabaseConnection, compact bool) func(context.Context, *mcpsdk.CallToolRequest, *transcriptSearchV2Args) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *transcriptSearchV2Args) (*mcpsdk.CallToolResult, any, error) {
		compiled, tsquery, err := compileClipQueries(args.Query, args.Queries)
		var groups [][]search.Result
		if len(args.QueryGroups) > 0 {
			if args.Query != "" || len(args.Queries) > 0 {
				return nil, nil, fmt.Errorf("use query_groups or query/queries, not both")
			}
			groups, compiled, tsquery, err = compileTopicGroups(args.QueryGroups)
		}
		if err != nil {
			return nil, nil, err
		}
		if args.MatchScope != "" && args.MatchScope != "cue" && args.MatchScope != "window" {
			return nil, nil, fmt.Errorf("match_scope must be cue or window")
		}
		if args.Offset < 0 || args.PassageOffset < 0 {
			return nil, nil, fmt.Errorf("continuation offsets must be nonnegative")
		}
		limit := args.Limit
		if limit <= 0 || limit > 100 {
			limit = 25
		}
		contextSeconds := args.ContextSeconds
		if contextSeconds <= 0 {
			contextSeconds = 60
		}
		if contextSeconds > 300 {
			return nil, nil, fmt.Errorf("context_seconds must be at most 300; use get_transcript for longer context")
		}
		creatorID, err := optionalUUID(args.CreatorID)
		if err != nil {
			return nil, nil, err
		}
		channelID, err := optionalUUID(args.ChannelID)
		if err != nil {
			return nil, nil, err
		}
		videoIDFilter, err := optionalUUID(args.VideoID)
		if err != nil {
			return nil, nil, err
		}
		rows, err := dbc.Queries(ctx).SearchTranscripts(ctx, &db.SearchTranscriptsParams{VideoID: videoIDFilter, Tsquery: tsquery, CreatorID: creatorID, ChannelID: channelID, PageLimit: 200, PageOffset: args.Offset})
		if err != nil {
			return nil, nil, err
		}
		ids := make([]pgtype.UUID, 0, len(rows))
		for _, r := range rows {
			ids = append(ids, r.VideoID)
		}
		windowRows, err := dbc.Queries(ctx).ListContextWindowsForVideos(ctx, ids)
		if err != nil {
			return nil, nil, err
		}
		byVideo := map[pgtype.UUID][]*db.ListContextWindowsForVideoRow{}
		for _, w := range windowRows {
			byVideo[w.VideoID] = append(byVideo[w.VideoID], (*db.ListContextWindowsForVideoRow)(w))
		}
		passages := make([]map[string]any, 0, limit)
		for rowIndex, row := range rows {
			transcript, err := dbc.Queries(ctx).GetVideoTranscriptByLanguage(ctx, &db.GetVideoTranscriptByLanguageParams{VideoID: row.VideoID, Lang: row.Lang})
			if err != nil {
				return nil, nil, err
			}
			cues, err := decodeTranscriptCues(transcript.Cues)
			if err != nil {
				return nil, nil, fmt.Errorf("video %s transcript: %w", uuidString(row.VideoID), err)
			}
			if len(cues) == 0 {
				continue
			}
			windows := byVideo[row.VideoID]
			hits := make([]passageCandidate, 0)
			for i, cue := range cues {
				evidence := matchedClipEvidence(cues, i, compiled)
				windowCues := cueWindow(cues, i, contextSeconds/2)
				if len(groups) > 0 || args.MatchScope == "window" {
					text := captions.PlainText(windowCues)
					if !matchesTopicGroups(text, groups, compiled) {
						continue
					}
					// Anchor on a cue contributing a topic, not an arbitrary earlier cue.
					anchor := false
					for _, query := range compiled {
						if query.Match(cue.Text) {
							anchor = true
							break
						}
					}
					if !anchor && len(groups) > 0 {
						continue
					}
					evidence = text
				}
				if evidence == "" {
					continue
				}
				start, end := windowCues[0].Start, windowCues[len(windowCues)-1].End
				hits = append(hits, passageCandidate{
					Timestamp:     cue.Start,
					ContextStart:  start,
					ContextEnd:    end,
					MatchEvidence: evidence,
					Cues:          windowCues,
					Windows:       containingSpans(windows, cue.Start),
				})
			}
			hits = skipNearbyPassages(collapsePassages(hits))
			videoID := uuidString(row.VideoID)
			for hitIndex, hit := range hits {
				if rowIndex == 0 && hitIndex < args.PassageOffset {
					continue
				}
				if len(passages) >= int(limit) {
					return clipSearchResult(passages, args.Offset+int32(rowIndex), hitIndex, true)
				}
				entry := map[string]any{
					"video_id": videoID, "title": row.Title, "uploader": row.Uploader,
					"timestamp": hit.Timestamp, "web_path": fmt.Sprintf("/videos/%s?t=%.3f", videoID, hit.Timestamp),
					"context_start": hit.ContextStart, "context_end": hit.ContextEnd, "cues": hit.Cues,
					"match_evidence": hit.MatchEvidence, "context_windows": windowsIntersecting(windows, hit.ContextStart, hit.ContextEnd),
					"media_status": map[bool]string{true: "catalog-only", false: "playable"}[row.Media == "metadata"],
				}
				if !compact && row.Media != "metadata" {
					entry["boundary_candidates"] = boundaryPair(videoID, hit.ContextStart, hit.ContextEnd, 5)
				}
				if compact {
					entry = compactClipCandidate(row, hit, windows)
				}
				entry["speaker_attribution"] = "unverified; uploader is channel ownership, not speaker identity"
				entry["transcript_coverage"] = json.RawMessage(transcript.Coverage)
				entry["transcript_complete"] = len(transcript.Coverage) == 0
				passages = append(passages, entry)
			}
		}
		return clipSearchResult(passages, args.Offset+int32(len(rows)), 0, len(rows) == 200)
	}
}

func cueWindow(cues []captions.Cue, index int, radius float64) []captions.Cue {
	lo, hi := cues[index].Start-radius, cues[index].End+radius
	start, end := index, index+1
	for start > 0 && cues[start-1].End >= lo {
		start--
	}
	for end < len(cues) && cues[end].Start <= hi {
		end++
	}
	return cues[start:end]
}

type contextSpan struct {
	ID    string
	Start float64
	End   float64
}

type passageCandidate struct {
	Timestamp     float64
	ContextStart  float64
	ContextEnd    float64
	MatchEvidence string
	Cues          []captions.Cue
	Windows       []contextSpan
}

func containingWindow(hit passageCandidate) *contextSpan {
	var best *contextSpan
	for i := range hit.Windows {
		w := &hit.Windows[i]
		if hit.Timestamp < w.Start || hit.Timestamp > w.End {
			continue
		}
		if best == nil || (w.End-w.Start) < (best.End-best.Start) {
			best = w
		}
	}
	return best
}

func collapseKey(hit passageCandidate) string {
	w := containingWindow(hit)
	if w == nil {
		return ""
	}
	if w.ID != "" {
		return w.ID
	}
	return fmt.Sprintf("%g:%g", w.Start, w.End)
}

// collapsePassages merges hits whose timestamps fall in the same Context Window
// [start,end] into one candidate. Hits with no containing window stay independent.
func collapsePassages(hits []passageCandidate) []passageCandidate {
	if len(hits) <= 1 {
		return append([]passageCandidate(nil), hits...)
	}
	index := make(map[string]int)
	out := make([]passageCandidate, 0, len(hits))
	for _, hit := range hits {
		key := collapseKey(hit)
		if key == "" {
			out = append(out, hit)
			continue
		}
		if i, ok := index[key]; ok {
			out[i] = mergePassageCandidates(out[i], hit)
			continue
		}
		index[key] = len(out)
		out = append(out, hit)
	}
	return out
}

func mergePassageCandidates(a, b passageCandidate) passageCandidate {
	if b.Timestamp < a.Timestamp {
		a.Timestamp = b.Timestamp
	}
	if b.ContextStart < a.ContextStart {
		a.ContextStart = b.ContextStart
	}
	if b.ContextEnd > a.ContextEnd {
		a.ContextEnd = b.ContextEnd
	}
	if a.MatchEvidence == "" {
		a.MatchEvidence = b.MatchEvidence
	}
	a.Cues = mergeCues(a.Cues, b.Cues)
	a.Windows = mergeSpans(a.Windows, b.Windows)
	return a
}

func mergeCues(a, b []captions.Cue) []captions.Cue {
	out := append(append([]captions.Cue{}, a...), b...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Start != out[j].Start {
			return out[i].Start < out[j].Start
		}
		return out[i].End < out[j].End
	})
	n := 0
	for _, c := range out {
		if n > 0 && out[n-1].Start == c.Start && out[n-1].End == c.End {
			continue
		}
		out[n] = c
		n++
	}
	return out[:n]
}

func mergeSpans(a, b []contextSpan) []contextSpan {
	out := append([]contextSpan{}, a...)
	seen := make(map[string]bool, len(a)+len(b))
	for _, w := range a {
		seen[spanKey(w)] = true
	}
	for _, w := range b {
		k := spanKey(w)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, w)
	}
	return out
}

func spanKey(w contextSpan) string {
	if w.ID != "" {
		return w.ID
	}
	return fmt.Sprintf("%g:%g", w.Start, w.End)
}

func skipNearbyPassages(hits []passageCandidate) []passageCandidate {
	lastEnd := -1.0
	out := make([]passageCandidate, 0, len(hits))
	for _, hit := range hits {
		if hit.Timestamp < lastEnd && containingWindow(hit) == nil {
			continue
		}
		if hit.ContextEnd > lastEnd {
			lastEnd = hit.ContextEnd
		}
		out = append(out, hit)
	}
	return out
}

func containingSpans(rows []*db.ListContextWindowsForVideoRow, ts float64) []contextSpan {
	out := make([]contextSpan, 0)
	for _, w := range rows {
		if w == nil || ts < w.StartTs || ts > w.EndTs {
			continue
		}
		out = append(out, contextSpan{ID: uuidString(w.ID), Start: w.StartTs, End: w.EndTs})
	}
	return out
}

func windowsIntersecting(rows []*db.ListContextWindowsForVideoRow, start, end float64) []*db.ListContextWindowsForVideoRow {
	out := make([]*db.ListContextWindowsForVideoRow, 0)
	for _, w := range rows {
		if w != nil && w.EndTs >= start && w.StartTs <= end {
			out = append(out, w)
		}
	}
	return out
}

type contextRangeArgs struct {
	VideoID string   `json:"video_id"`
	Start   *jsnum.F `json:"start,omitempty"`
	End     *jsnum.F `json:"end,omitempty"`
}

func getContextWindows(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *contextRangeArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *contextRangeArgs) (*mcpsdk.CallToolResult, any, error) {
		id, err := parseUUID(args.VideoID)
		if err != nil {
			return nil, nil, err
		}
		var start, end *float64
		if args.Start != nil {
			s := float64(*args.Start)
			start = &s
		}
		if args.End != nil {
			e := float64(*args.End)
			end = &e
		}
		rows, err := dbc.Queries(ctx).ListContextWindowsForVideo(ctx, &db.ListContextWindowsForVideoParams{VideoID: id, StartTs: start, EndTs: end})
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(nestContextWindows(rows))
	}
}

type nestedContextWindow struct {
	*db.ListContextWindowsForVideoRow
	Shorts []*db.ListContextWindowsForVideoRow `json:"shorts"`
}

func nestContextWindows(rows []*db.ListContextWindowsForVideoRow) []nestedContextWindow {
	children := map[[16]byte][]*db.ListContextWindowsForVideoRow{}
	var parents []*db.ListContextWindowsForVideoRow
	for _, r := range rows {
		if r == nil {
			continue
		}
		if r.Kind == "short" && r.ParentID.Valid {
			children[r.ParentID.Bytes] = append(children[r.ParentID.Bytes], r)
			continue
		}
		if r.Kind == "short" {
			continue
		}
		parents = append(parents, r)
	}
	out := make([]nestedContextWindow, 0, len(parents))
	for _, p := range parents {
		shorts := children[p.ID.Bytes]
		if shorts == nil {
			shorts = []*db.ListContextWindowsForVideoRow{}
		}
		out = append(out, nestedContextWindow{ListContextWindowsForVideoRow: p, Shorts: shorts})
	}
	return out
}

type boundaryArgs struct {
	VideoID       string  `json:"video_id"`
	Start         jsnum.F `json:"start"`
	End           jsnum.F `json:"end"`
	RadiusSeconds jsnum.F `json:"radius_seconds,omitempty"`
}

func suggestClipBoundaries(_ *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *boundaryArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(_ context.Context, _ *mcpsdk.CallToolRequest, args *boundaryArgs) (*mcpsdk.CallToolResult, any, error) {
		if _, err := parseUUID(args.VideoID); err != nil {
			return nil, nil, err
		}
		radius := float64(args.RadiusSeconds)
		if radius <= 0 {
			radius = 5
		}
		return jsonResult(boundaryPair(args.VideoID, float64(args.Start), float64(args.End), radius))
	}
}

func boundaryPair(videoID string, start, end, radius float64) map[string]any {
	data, err := os.ReadFile(filepath.Join("/downloads", videoID, "waveform", "peaks.i16"))
	if err != nil {
		return map[string]any{"quality": "cue", "start": []any{map[string]any{"time_seconds": start}}, "end": []any{map[string]any{"time_seconds": end}}}
	}
	peaks := waveform.DecodeI16LE(data)
	return map[string]any{"quality": "waveform", "start": waveform.Suggest(peaks, .1, start, radius, 30, 3), "end": waveform.Suggest(peaks, .1, end, radius, 30, 3)}
}

type createContextArgs struct {
	VideoID     string          `json:"video_id"`
	Start       float64         `json:"start"`
	End         float64         `json:"end"`
	Title       string          `json:"title"`
	Summary     string          `json:"summary,omitempty"`
	Topics      []string        `json:"topics,omitempty"`
	Entities    []string        `json:"entities,omitempty"`
	Evidence    json.RawMessage `json:"evidence,omitempty"`
	SourceQuery string          `json:"source_query,omitempty"`
}

func createContextWindowMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *createContextArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *createContextArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		id, err := parseUUID(args.VideoID)
		if err != nil {
			return nil, nil, err
		}
		video, err := dbc.Queries(ctx).GetVideoByID(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		if err := contextwindow.Validate(args.Start, args.End, args.Title, video.DurationSeconds); err != nil {
			return nil, nil, err
		}
		evidence := args.Evidence
		if len(evidence) == 0 {
			evidence = json.RawMessage(`[]`)
		}
		if args.Topics == nil {
			args.Topics = []string{}
		}
		if args.Entities == nil {
			args.Entities = []string{}
		}
		row, err := dbc.Queries(ctx).CreateContextWindow(ctx, &db.CreateContextWindowParams{VideoID: id, StartTs: args.Start, EndTs: args.End, Title: strings.TrimSpace(args.Title), Summary: args.Summary, Topics: args.Topics, Entities: args.Entities, Origin: "mcp", SourceQuery: args.SourceQuery, TranscriptCueEvidence: evidence, BoundaryQuality: "cue", CreatedBy: tokenFrom(ctx).UserID})
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(row)
	}
}

type updateContextArgs struct {
	ContextWindowID string   `json:"context_window_id"`
	Start           *float64 `json:"start,omitempty"`
	End             *float64 `json:"end,omitempty"`
	Title           *string  `json:"title,omitempty"`
	Summary         *string  `json:"summary,omitempty"`
	Topics          []string `json:"topics,omitempty"`
	Entities        []string `json:"entities,omitempty"`
}

func updateContextWindowMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *updateContextArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *updateContextArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		id, err := parseUUID(a.ContextWindowID)
		if err != nil {
			return nil, nil, err
		}
		row, err := contextwindow.Edit(ctx, dbc, id, contextwindow.Patch{Start: a.Start, End: a.End, Title: a.Title, Summary: a.Summary, Topics: a.Topics, Entities: a.Entities})
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(row)
	}
}

type indexCatalogArgs struct {
	ID      string `json:"channel_id"`
	Refresh bool   `json:"refresh,omitempty"`
}

func indexChannelCatalogMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *indexCatalogArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *indexCatalogArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		resolved, err := lookupChannel(ctx, dbc.Queries(ctx), args.ID)
		if err != nil {
			return nil, nil, err
		}
		if resolved.Row == nil {
			return nil, nil, fmt.Errorf("channel row required")
		}
		rows, err := catalog.IndexChannel(ctx, dbc.Queries(ctx), resolved.Row, tokenFrom(ctx).UserID, args.Refresh)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(rows)
	}
}

type indexCreatorCatalogArgs struct {
	CreatorID string `json:"creator_id"`
	Refresh   bool   `json:"refresh,omitempty"`
}

func indexCreatorCatalogMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *indexCreatorCatalogArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *indexCreatorCatalogArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		id, err := parseUUID(args.CreatorID)
		if err != nil {
			return nil, nil, err
		}
		rows, err := catalog.IndexCreator(ctx, dbc.Queries(ctx), id, tokenFrom(ctx).UserID, args.Refresh)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(rows)
	}
}

type planSegmentInput struct {
	VideoID            string  `json:"video_id"`
	Start              float64 `json:"start"`
	End                float64 `json:"end"`
	ContextWindowID    string  `json:"context_window_id,omitempty" jsonschema:"Existing context window UUID. Omit when none was returned; never send the string null."`
	MatchEvidence      any     `json:"match_evidence,omitempty" jsonschema:"JSON evidence object copied from candidate.segment.match_evidence; preserve text, timestamp, and language."`
	SelectionRationale string  `json:"selection_rationale,omitempty"`
}
type savePlanArgs struct {
	Title             string             `json:"title"`
	CreatorID         string             `json:"creator_id,omitempty"`
	Query             string             `json:"query"`
	Segments          []planSegmentInput `json:"segments"`
	SortChronological bool               `json:"sort_chronological,omitempty" jsonschema:"Order segments by upload date then timestamp"`
	MergeNearby       bool               `json:"merge_nearby,omitempty" jsonschema:"Merge same-video segments within 15 seconds"`
}
type updatePlanArgs struct {
	Revision          int32              `json:"revision" jsonschema:"Expected current plan revision"`
	PlanID            string             `json:"plan_id"`
	Segments          []planSegmentInput `json:"segments"`
	SortChronological bool               `json:"sort_chronological,omitempty"`
	MergeNearby       bool               `json:"merge_nearby,omitempty"`
}

func saveCompilationPlan(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *savePlanArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *savePlanArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		creatorID, err := optionalUUID(args.CreatorID)
		if err != nil {
			return nil, nil, err
		}
		q, tx, err := dbc.NewWithTX(ctx)
		if err != nil {
			return nil, nil, err
		}
		defer tx.Rollback(ctx)
		plan, err := q.CreateCompilationPlan(ctx, &db.CreateCompilationPlanParams{CreatedBy: tokenFrom(ctx).UserID, CreatorID: creatorID, SourceQuery: args.Query, Title: strings.TrimSpace(args.Title)})
		if err != nil {
			return nil, nil, err
		}
		duration, err := replacePlanSegments(ctx, q, plan.ID, args.Segments, args.SortChronological, args.MergeNearby)
		if err != nil {
			return nil, nil, err
		}
		plan, err = q.SetInitialCompilationPlanDuration(ctx, &db.SetInitialCompilationPlanDurationParams{ID: plan.ID, EstimatedDuration: duration})
		if err != nil {
			return nil, nil, err
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, nil, err
		}
		return jsonResult(plan)
	}
}
func updateCompilationPlan(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *updatePlanArgs) (*mcpsdk.CallToolResult, any, error) {
	return editCompilationPlan(dbc, false)
}

func editCompilationPlan(dbc *db.DatabaseConnection, appendSegments bool) func(context.Context, *mcpsdk.CallToolRequest, *updatePlanArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *updatePlanArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		id, err := parseUUID(args.PlanID)
		if err != nil {
			return nil, nil, err
		}
		q, tx, err := dbc.NewWithTX(ctx)
		if err != nil {
			return nil, nil, err
		}
		defer tx.Rollback(ctx)
		current, err := q.LockCompilationPlan(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		if current.CreatedBy != tokenFrom(ctx).UserID {
			return nil, nil, fmt.Errorf("plan access denied")
		}
		if current.Revision != args.Revision {
			return nil, nil, fmt.Errorf("revision conflict: current revision is %d", current.Revision)
		}
		segments := args.Segments
		if appendSegments {
			if len(segments) == 0 || len(segments) > 100 {
				return nil, nil, fmt.Errorf("append 1–100 segments per call")
			}
			existing, err := q.ListCompilationPlanSegments(ctx, id)
			if err != nil {
				return nil, nil, err
			}
			combined := make([]planSegmentInput, 0, len(existing)+len(segments))
			for _, s := range existing {
				combined = append(combined, planSegmentInput{VideoID: uuidString(s.VideoID), Start: s.StartTs, End: s.EndTs, ContextWindowID: uuidString(s.ContextWindowID), MatchEvidence: json.RawMessage(s.MatchEvidence), SelectionRationale: s.SelectionRationale})
			}
			segments = append(combined, segments...)
		}
		if err = q.ClearCompilationPlanSegments(ctx, id); err != nil {
			return nil, nil, err
		}
		duration, err := replacePlanSegments(ctx, q, id, segments, args.SortChronological, args.MergeNearby)
		if err != nil {
			return nil, nil, err
		}
		plan, err := q.BumpCompilationPlanRevision(ctx, &db.BumpCompilationPlanRevisionParams{ID: id, EstimatedDuration: duration})
		if err != nil {
			return nil, nil, err
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, nil, err
		}
		return jsonResult(plan)
	}
}

type datedPlanSegment struct {
	in   planSegmentInput
	id   pgtype.UUID
	date pgtype.Date
}

func replacePlanSegments(ctx context.Context, q *db.Queries, planID pgtype.UUID, segments []planSegmentInput, chronological, mergeNearby bool) (float64, error) {
	if len(segments) == 0 {
		return 0, fmt.Errorf("at least one segment is required")
	}
	list := make([]datedPlanSegment, 0, len(segments))
	for _, in := range segments {
		id, err := parseUUID(in.VideoID)
		if err != nil {
			return 0, err
		}
		v, err := q.GetVideoByID(ctx, id)
		if err != nil {
			return 0, err
		}
		if in.Start < 0 || in.End <= in.Start || (v.DurationSeconds != nil && in.End > float64(*v.DurationSeconds)+.5) {
			return 0, fmt.Errorf("invalid segment bounds for %s", in.VideoID)
		}
		list = append(list, datedPlanSegment{in: in, id: id, date: v.UploadDate})
	}
	merged := list
	if chronological || mergeNearby {
		merged = orderPlanSegments(list, chronological, mergeNearby)
	}
	total := 0.0
	for i, item := range merged {
		cw, err := optionalUUID(item.in.ContextWindowID)
		if err != nil {
			return 0, err
		}
		evidence, marshalErr := json.Marshal(item.in.MatchEvidence)
		if marshalErr != nil {
			return 0, marshalErr
		}
		if item.in.MatchEvidence == nil {
			evidence = []byte(`{}`)
		}
		_, err = q.AddCompilationPlanSegment(ctx, &db.AddCompilationPlanSegmentParams{PlanID: planID, Position: int32(i), VideoID: item.id, StartTs: item.in.Start, EndTs: item.in.End, ContextWindowID: cw, MatchEvidence: evidence, SelectionRationale: item.in.SelectionRationale})
		if err != nil {
			return 0, err
		}
		total += item.in.End - item.in.Start
	}
	return total, nil
}

func orderPlanSegments(list []datedPlanSegment, chronological, mergeNearby bool) []datedPlanSegment {
	if chronological {
		list = sortPlanSegments(list)
	}
	if mergeNearby {
		return mergeNearbyPlanSegments(list)
	}
	return list
}

func sortPlanSegments(list []datedPlanSegment) []datedPlanSegment {
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].date.Valid != list[j].date.Valid {
			return list[i].date.Valid
		}
		if list[i].date.Valid && !list[i].date.Time.Equal(list[j].date.Time) {
			return list[i].date.Time.Before(list[j].date.Time)
		}
		if list[i].in.VideoID == list[j].in.VideoID {
			return list[i].in.Start < list[j].in.Start
		}
		return list[i].in.VideoID < list[j].in.VideoID
	})
	return list
}

func mergeNearbyPlanSegments(list []datedPlanSegment) []datedPlanSegment {
	merged := make([]datedPlanSegment, 0, len(list))
	for _, item := range list {
		if len(merged) > 0 {
			last := &merged[len(merged)-1]
			if last.in.VideoID == item.in.VideoID && item.in.Start-last.in.End <= 15 {
				if item.in.End > last.in.End {
					last.in.End = item.in.End
				}
				continue
			}
		}
		merged = append(merged, item)
	}
	return merged
}

type planIDArgs struct {
	PlanID   string `json:"plan_id"`
	Revision int32  `json:"revision,omitempty" jsonschema:"Required expected revision when executing"`
	Retry    bool   `json:"retry,omitempty" jsonschema:"Retry failed downloads or render for this revision"`
}

func listCompilationPlans(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *struct{}) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ *struct{}) (*mcpsdk.CallToolResult, any, error) {
		tok := tokenFrom(ctx)
		if tok == nil {
			return nil, nil, fmt.Errorf("authentication required")
		}
		rows, err := dbc.Queries(ctx).ListCompilationPlansForUser(ctx, &db.ListCompilationPlansForUserParams{CreatedBy: tok.UserID, PageLimit: 100})
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(rows)
	}
}

func getCompilationPlan(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *planIDArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *planIDArgs) (*mcpsdk.CallToolResult, any, error) {
		id, err := parseUUID(a.PlanID)
		if err != nil {
			return nil, nil, err
		}
		q := dbc.Queries(ctx)
		plan, err := q.GetCompilationPlan(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		if tokenFrom(ctx) == nil || plan.CreatedBy != tokenFrom(ctx).UserID {
			return nil, nil, fmt.Errorf("plan access denied")
		}
		segments, err := q.ListCompilationPlanSegments(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		executions, err := q.ListCompilationExecutions(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{"plan": plan, "segments": segments, "executions": executions})
	}
}
func createCompilation(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *planIDArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *planIDArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		id, err := parseUUID(a.PlanID)
		if err != nil {
			return nil, nil, err
		}
		execution, err := compilation.Execute(ctx, dbc, id, tokenFrom(ctx).UserID, a.Revision, a.Retry)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(execution)
	}
}

func optionalUUID(value string) (pgtype.UUID, error) {
	if strings.TrimSpace(value) == "" {
		return pgtype.UUID{}, nil
	}
	return parseUUID(value)
}

const maxTranscriptCueBytes = 8 << 20

func decodeTranscriptCues(raw []byte) ([]captions.Cue, error) {
	if len(raw) > maxTranscriptCueBytes {
		return nil, fmt.Errorf("transcript cues exceed %d bytes", maxTranscriptCueBytes)
	}
	var cues []captions.Cue
	if err := json.Unmarshal(raw, &cues); err != nil {
		return nil, err
	}
	return cues, nil
}

// matchedCueEvidence anchors a match to its first contributing cue, before adding surrounding context.
func matchedCueEvidence(cues []captions.Cue, index int, compiled search.Result) string {
	for end := index + 1; end <= len(cues) && end <= index+3; end++ {
		text := captions.PlainText(cues[index:end])
		if !compiled.Match(text) {
			continue
		}
		if end > index+1 && compiled.Match(captions.PlainText(cues[index+1:end])) {
			return ""
		}
		return text
	}
	return ""
}
