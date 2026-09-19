package mcp

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/stitch"
)

func TestTeaserToolsOnMCPWire(t *testing.T) {
	srv := newServer(nil)
	a, b := mcpsdk.NewInMemoryTransports()
	ss, err := srv.Connect(context.Background(), a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "fixture", Version: "test"}, nil)
	cs, err := client.Connect(context.Background(), b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	listed, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"get_shortform_workflow": true, "suggest_teasers": true, "create_teaser": true, "set_teaser_layout": true, "check_teaser": true, "style_teaser_captions": true}
	for _, tool := range listed.Tools {
		delete(want, tool.Name)
	}
	for missing := range want {
		t.Errorf("missing teaser tool %s", missing)
	}
	guide, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "get_shortform_workflow", Arguments: map[string]any{}})
	if err != nil || guide.IsError || len(guide.Content) == 0 {
		t.Fatalf("shortform workflow unavailable: %v", err)
	}
}

func TestTeaserCandidateFromShortUsesNestedBounds(t *testing.T) {
	parentID := mustTestUUID("11111111-1111-1111-1111-111111111111")
	shortID := mustTestUUID("22222222-2222-2222-2222-222222222222")
	videoID := mustTestUUID("33333333-3333-3333-3333-333333333333")
	parent := nestedContextWindow{ListContextWindowsForVideoRow: &db.ListContextWindowsForVideoRow{ID: parentID, VideoID: videoID, Title: "Studio Banter", Kind: "window"}}
	sh := &db.ListContextWindowsForVideoRow{ID: shortID, VideoID: videoID, ParentID: parentID, Title: "Casio punch", Hook: "that's the joke", StartTs: 12, EndTs: 28, Kind: "short"}
	got := teaserCandidateFromShort(parent, sh)
	if got["kind"] != "short" {
		t.Fatalf("kind: %v", got["kind"])
	}
	seg, ok := got["segment"].(planSegmentInput)
	if !ok || seg.Start != 12 || seg.End != 28 || seg.ContextWindowID != shortID.String() {
		t.Fatalf("segment: %+v", got["segment"])
	}
	if got["title"] != "Studio Banter · Casio punch" {
		t.Fatalf("title: %v", got["title"])
	}
}

func mustTestUUID(s string) pgtype.UUID {
	var id pgtype.UUID
	if err := id.Scan(s); err != nil {
		panic(err)
	}
	return id
}

func TestTeaserLayoutValidation(t *testing.T) {
	valid := []stitch.TeaserLayout{
		{Mode: "single_speaker", Crops: []stitch.TeaserCrop{{X: 0, Y: 0, Width: 1, Height: 1}}},
		{Mode: "two_speakers", Crops: []stitch.TeaserCrop{{X: 0, Y: 0, Width: .5, Height: 1}, {X: .5, Y: 0, Width: .5, Height: 1}}},
		{Mode: "preserve_scene"},
	}
	for _, layout := range valid {
		if err := validateTeaserLayout(layout); err != nil {
			t.Errorf("valid layout rejected: %+v: %v", layout, err)
		}
	}
	for _, layout := range []stitch.TeaserLayout{
		{Mode: "unknown"},
		{Mode: "single_speaker"},
		{Mode: "preserve_scene", Crops: []stitch.TeaserCrop{{X: 0, Y: 0, Width: 1, Height: 1}, {X: 0, Y: 0, Width: 1, Height: 1}}},
		{Mode: "single_speaker", Crops: []stitch.TeaserCrop{{X: .9, Y: 0, Width: .2, Height: 1}}},
	} {
		if err := validateTeaserLayout(layout); err == nil {
			t.Errorf("invalid layout accepted: %+v", layout)
		}
	}
}
