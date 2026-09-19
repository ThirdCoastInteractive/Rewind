package encode

import (
	"encoding/json"
	"testing"

	"thirdcoast.systems/rewind/internal/stitch"
)

func TestCanonicalLegacySegmentsPreservesFrozenFields(t *testing.T) {
	legacyTitle, _ := json.Marshal(map[string]any{"type": "title", "text": "Chapter", "subtitle": "One", "bg_color": "#123456", "font": "Test", "duration": 99})
	legacyClip, _ := json.Marshal(map[string]any{
		"title": "Frozen clip", "gain_db": -2.5, "look": "warm",
		"legacy_metadata": map[string]any{
			"crops":        []map[string]any{{"id": "portrait", "x": .1, "y": .2, "width": .7, "height": .8}},
			"filter_stack": []map[string]any{{"type": "brightness", "params": map[string]any{"value": .2}}},
		},
	})
	snapshot := stitch.RenderSnapshot{Resolved: []stitch.ResolvedRenderSegment{
		{ID: "gap", Type: "gap", DurationUS: 2_000_000},
		{ID: "title", Type: "title", DurationUS: 3_000_000, Legacy: legacyTitle},
		{ID: "clip", Type: "clip", VideoID: "video-1", SourceInUS: 4_000_000, DurationUS: 5_000_000, Legacy: legacyClip, Transition: &stitch.Transition{Kind: "fade", DurationUS: 500_000}},
		{ID: "export", Type: "stitch", ExportJobID: "job-1", SourceInUS: 9_000_000, DurationUS: 1_500_000},
	}}
	got, err := canonicalLegacySegments(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 || got[0].Type != "title" || got[0].BgColor != "#000000" || float64(got[0].Duration) != 2 {
		t.Fatalf("gap conversion = %#v", got[0])
	}
	if got[1].Type != "title" || got[1].Text != "Chapter" || got[1].Subtitle != "One" || float64(got[1].Duration) != 3 {
		t.Fatalf("title conversion = %#v", got[1])
	}
	clip := got[2]
	if clip.Type != "video" || clip.VideoID != "video-1" || clip.ClipID != "" || float64(clip.StartTs) != 4 || float64(clip.EndTs) != 9 || float64(clip.Duration) != 5 || float64(clip.GainDb) != -2.5 || clip.Look != "warm" {
		t.Fatalf("clip conversion = %#v", clip)
	}
	if len(clip.Crops) != 1 || len(clip.Filters) != 1 || clip.Filters[0].Type != "brightness" {
		t.Fatalf("frozen metadata not preserved: %#v", clip)
	}
	var tr stitchTransitionJSON
	if err := json.Unmarshal(clip.RawTransition, &tr); err != nil || tr.Type != "fade" || float64(tr.Duration) != .5 {
		t.Fatalf("transition = %s (%v)", clip.RawTransition, err)
	}
	if got[3].Type != "stitch" || got[3].ExportJobID != "job-1" || float64(got[3].Duration) != 1.5 {
		t.Fatalf("stitch conversion = %#v", got[3])
	}
}

func TestCanonicalLegacySegmentsCopiesMulticamShots(t *testing.T) {
	snapshot := stitch.RenderSnapshot{Resolved: []stitch.ResolvedRenderSegment{{
		ID: "clip", Type: "clip", VideoID: "video-1",
		SourceInUS: 10_000_000, DurationUS: 5_000_000, ClipStartUS: 8_000_000,
		Crops: []stitch.CameraCrop{{ID: "camA", X: .25, Y: .5, Width: .4, Height: .8}},
		Shots: []stitch.CameraShot{{CropID: "camA", Start: 0, End: 3}, {CropID: "camA", Start: 3, End: 7}},
	}}}
	got, err := canonicalLegacySegments(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	seg := got[0]
	if seg.ClipID != "" || seg.ClipStartUS != 8_000_000 || len(seg.Crops) != 1 || len(seg.Shots) != 2 {
		t.Fatalf("multicam fields not copied from snapshot: %#v", seg)
	}
	if seg.Crops[0].ID != "camA" || seg.Shots[0].CropID != "camA" || seg.Shots[1].End != 7 {
		t.Fatalf("shots = %#v crops = %#v", seg.Shots, seg.Crops)
	}
}

func TestInputRelativeShotsDropsBeforeInPoint(t *testing.T) {
	shots := encoderShots([]stitch.CameraShot{
		{CropID: "a", Start: 0, End: 2},
		{CropID: "b", Start: 2, End: 5},
		{CropID: "c", Start: 5, End: 8},
	})
	// Clip starts at 10s; segment is left-trimmed to sourceIn=13s for 3s.
	got := inputRelativeShots(shots, 10_000_000, 13, 3)
	if len(got) != 2 {
		t.Fatalf("expected 2 remaining shots, got %#v", got)
	}
	if got[0].CropID != "b" || got[0].Start != 0 || got[0].End != 2 {
		t.Fatalf("first remaining = %#v", got[0])
	}
	if got[1].CropID != "c" || got[1].Start != 2 || got[1].End != 3 {
		t.Fatalf("second remaining = %#v", got[1])
	}
}

func TestCanonicalLegacySegmentsRejectsUnknownType(t *testing.T) {
	_, err := canonicalLegacySegments(stitch.RenderSnapshot{Resolved: []stitch.ResolvedRenderSegment{{ID: "x", Type: "mystery", DurationUS: 1}}})
	if err == nil {
		t.Fatal("expected unsupported segment error")
	}
}
