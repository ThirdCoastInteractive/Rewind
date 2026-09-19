package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/captions"
)

func TestCreateClipArgsAcceptsQuotedSeconds(t *testing.T) {
	var a createClipArgs
	if err := json.Unmarshal([]byte(`{"video_id":"x","start":"10.5","end":"20","title":"t"}`), &a); err != nil {
		t.Fatal(err)
	}
	if float64(a.Start) != 10.5 || float64(a.End) != 20 {
		t.Fatalf("start=%v end=%v", a.Start, a.End)
	}
}

func TestClipAlternativeQueries(t *testing.T) {
	queries, ts, err := compileClipQueries("", []string{"Adam", `"Adam Sellers"`, "hot tub"})
	if err != nil || !strings.Contains(ts, " | ") {
		t.Fatal(ts, err)
	}
	for _, text := range []string{"Adam is here", "Adam Sellers", "the hot tubs are empty"} {
		if matchedClipEvidence([]captions.Cue{{Text: text}}, 0, queries) == "" {
			t.Fatalf("missed %q", text)
		}
	}
	if matchedClipEvidence([]captions.Cue{{Text: "unrelated story"}}, 0, queries) != "" {
		t.Fatal("unrelated hit")
	}
	for _, bad := range [][]string{nil, {""}, {"-Adam"}} {
		if _, _, err := compileClipQueries("", bad); err == nil {
			t.Fatal("invalid queries accepted", bad)
		}
	}
	if _, _, err := compileClipQueries("Adam", []string{"hot tub"}); err == nil {
		t.Fatal("ambiguous query arguments accepted")
	}
}

func TestClippingWorkflowMentionsCaptionIndexAndThroughline(t *testing.T) {
	for _, s := range []string{"get_index_status", "index_url", "get_related", "playable", "across multiple weeks"} {
		if !strings.Contains(clippingWorkflow, s) {
			t.Fatalf("workflow missing %q", s)
		}
	}
	if !strings.Contains(clippingInstructions, "get_index_status") {
		t.Fatal("instructions missing get_index_status")
	}
}

func TestClippingToolsOnMCPWire(t *testing.T) {
	ctx := context.Background()
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "rewind", Version: "test"}, &mcpsdk.ServerOptions{Instructions: clippingInstructions})
	registerClippingTools(srv, nil)
	a, b := mcpsdk.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "generic-agent", Version: "test"}, nil)
	cs, err := client.Connect(ctx, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	listed, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range listed.Tools {
		names[tool.Name] = true
	}
	for _, name := range []string{"get_clipping_workflow", "find_clip_candidates", "create_stitch_project", "get_stitch_project", "update_stitch_project", "append_compilation_segments", "create_clip"} {
		if !names[name] {
			t.Fatalf("missing clipping tool %s", name)
		}
	}
	guide, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{Name: "get_clipping_workflow", Arguments: map[string]any{}})
	if err != nil || guide.IsError || len(guide.Content) == 0 {
		t.Fatal("guide unavailable", err)
	}
	// Validation and authorization must fail before touching the nil database.
	for _, call := range []*mcpsdk.CallToolParams{
		{Name: "find_clip_candidates", Arguments: map[string]any{"queries": []string{"-Adam"}}},
		{Name: "create_stitch_project", Arguments: map[string]any{"plan_id": "00000000-0000-0000-0000-000000000001", "revision": 1}},
		{Name: "update_stitch_project", Arguments: map[string]any{"project_id": "00000000-0000-0000-0000-000000000001", "title": "x"}},
		{Name: "update_stitch_project", Arguments: map[string]any{"project_id": "00000000-0000-0000-0000-000000000001", "description": "x"}},
	} {
		result, err := cs.CallTool(ctx, call)
		if err == nil && !result.IsError {
			t.Fatalf("accepted invalid or unauthorized call: %s", call.Name)
		}
	}
	prompt, err := cs.GetPrompt(ctx, &mcpsdk.GetPromptParams{Name: "compile_spoken_moments", Arguments: map[string]string{"request": "Find Adam and create a stitch project"}})
	if err != nil || len(prompt.Messages) == 0 {
		t.Fatal("prompt unavailable", err)
	}
}

func TestSpokenBeatBoundsAroundMatchingCue(t *testing.T) {
	hit := passageCandidate{
		Timestamp:    10,
		ContextStart: 5,
		ContextEnd:   65,
		Cues: []captions.Cue{
			{Start: 5, End: 7, Text: "earlier talk"},
			{Start: 10, End: 12, Text: "Adam Sellers"},
			{Start: 20, End: 22, Text: "later talk"},
			{Start: 60, End: 65, Text: "far later"},
		},
		Windows: []contextSpan{{ID: "cw1", Start: 5, End: 20}},
	}
	start, end := spokenBeatBounds(hit, 180)
	if start != 10-0.15 || end != 12+0.25 {
		t.Fatalf("beat %v-%v, want 9.85-12.25 not a 5-20 window", start, end)
	}
}

func TestSpokenBeatBoundsDoesNotSwallowCueWindow(t *testing.T) {
	hit := passageCandidate{
		Timestamp:    10,
		ContextStart: 0,
		ContextEnd:   60,
		Cues: []captions.Cue{
			{Start: 0, End: 2, Text: "open"},
			{Start: 5, End: 7, Text: "earlier"},
			{Start: 10, End: 12, Text: "Adam Sellers"},
			{Start: 15, End: 17, Text: "later"},
			{Start: 30, End: 32, Text: "mid"},
			{Start: 55, End: 60, Text: "end"},
		},
	}
	start, end := spokenBeatBounds(hit, 180)
	if start < 9.5 || end > 13 || end-start > 5 {
		t.Fatalf("swallowed 60s cue window: %v-%v", start, end)
	}
}

func TestSpokenBeatBoundsCompletesOneFollowingCue(t *testing.T) {
	hit := passageCandidate{
		Timestamp: 10,
		Cues: []captions.Cue{
			{Start: 10, End: 12, Text: "Adam"},
			{Start: 12.2, End: 14, Text: "Sellers"},
			{Start: 14.3, End: 16, Text: "again"},
		},
	}
	start, end := spokenBeatBounds(hit, 180)
	if start != 10-0.15 || end != 14+0.25 {
		t.Fatalf("expected matching cue plus one follower, got %v-%v", start, end)
	}
}

func TestSpokenBeatBoundsPointLikeYouTubeCues(t *testing.T) {
	hit := passageCandidate{
		Timestamp: 6.769,
		Cues: []captions.Cue{
			{Start: 6.769, End: 6.779, Text: "let's talk about my next car because I"},
			{Start: 9.53, End: 9.54, Text: "have my Lisa's up on my test lid like"},
			{Start: 10.82, End: 10.83, Text: "really I could buy could trade it in"},
		},
	}
	start, end := spokenBeatBounds(hit, 435)
	if start < 6.5 || start > 6.77 {
		t.Fatalf("start %v", start)
	}
	// One line: until the next cue starts, not a 10ms stub and not the following claim.
	if end < 9.5 || end > 9.9 {
		t.Fatalf("point cue should last until the next line, got %v-%v", start, end)
	}
}

func TestSpokenBeatBoundsNoCueFallsBack(t *testing.T) {
	start, end := spokenBeatBounds(passageCandidate{Timestamp: 40}, 50)
	if start != 40 || end != 48 {
		t.Fatalf("fallback %v-%v", start, end)
	}
	start, end = spokenBeatBounds(passageCandidate{Timestamp: 48}, 50)
	if start != 48 || end != 50 {
		t.Fatalf("clamped fallback %v-%v", start, end)
	}
}

func TestCompactClipCandidateUsesSpokenBeat(t *testing.T) {
	dur := int32(180)
	row := &db.SearchTranscriptsRow{Title: "fixture", Uploader: "Jeremy", Media: "video", DurationSeconds: &dur}
	hit := passageCandidate{
		Timestamp:     10,
		ContextStart:  5,
		ContextEnd:    65,
		MatchEvidence: "Adam Sellers",
		Cues: []captions.Cue{
			{Start: 5, End: 7, Text: "earlier"},
			{Start: 10, End: 12, Text: "Adam Sellers"},
			{Start: 60, End: 65, Text: "later"},
		},
		Windows: []contextSpan{{ID: "cw1", Start: 5, End: 20}},
	}
	got := compactClipCandidate(row, hit, []*db.ListContextWindowsForVideoRow{{StartTs: 5, EndTs: 20, Title: "Adam discussion"}})
	if got["bounds_source"] != "spoken_beat" {
		t.Fatalf("bounds_source %v", got["bounds_source"])
	}
	seg := got["segment"].(planSegmentInput)
	if seg.Start != 10-0.15 || seg.End != 12+0.25 {
		t.Fatalf("segment %v-%v, want spoken beat not 5-20", seg.Start, seg.End)
	}
	if seg.ContextWindowID != "cw1" {
		t.Fatalf("window citation lost: %q", seg.ContextWindowID)
	}
	if got["context"] == "" || len(got["context_windows"].([]map[string]any)) == 0 {
		t.Fatal("missing context metadata")
	}
	if got["duration_seconds"] != dur {
		t.Fatalf("duration_seconds %v", got["duration_seconds"])
	}
}
