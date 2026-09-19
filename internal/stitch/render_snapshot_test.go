package stitch

import (
	"encoding/json"
	"testing"
)

func TestCompileRenderSnapshotAddsGapsAndPreservesFrozenMetadata(t *testing.T) {
	d := Document{Version: CurrentVersion, FPS: 30, Width: 1920, Height: 1080, Segments: []Segment{{ID: "a", Type: "clip", StartUS: 0, DurationUS: 1_000_000, SourceInUS: 7_000_000, Legacy: []byte(`{"crops":[1]}`)}, {ID: "b", Type: "clip", StartUS: 2_000_000, DurationUS: 500_000, SourceInUS: 9_000_000}}}
	s, err := BuildRenderSnapshot(d, RenderOptions{Format: "mp4", Quality: "high", CaptionMode: "none", Scope: "all"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := CompileRenderSnapshot(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 || out[1]["type"] != "gap" {
		t.Fatalf("unexpected compiled output: %#v", out)
	}
	if string(out[0]["legacy"].(json.RawMessage)) != "{\"crops\":[1]}" {
		t.Fatal("metadata changed")
	}
}

func TestRenderSnapshotFreezesSegmentLayout(t *testing.T) {
	d := Document{Version: CurrentVersion, FPS: 30, Width: 1080, Height: 1920, Segments: []Segment{{ID: "a", Type: "clip", StartUS: 0, DurationUS: 1_000_000, Layout: &TeaserLayout{Mode: TeaserLayoutSingleSpeaker, Crops: []TeaserCrop{{X: .1, Y: .2, Width: .4, Height: .6}}}}}}
	s, err := BuildRenderSnapshot(d, RenderOptions{Format: "mp4", Quality: "high", CaptionMode: "none", Scope: "all"})
	if err != nil {
		t.Fatal(err)
	}
	d.Segments[0].Layout.Crops[0].X = .9
	if s.Resolved[0].Layout == nil || s.Resolved[0].Layout.Crops[0].X != .1 {
		t.Fatalf("layout was not frozen: %#v", s.Resolved[0].Layout)
	}
}

func TestRenderSnapshotFreezesMulticam(t *testing.T) {
	d := Document{Version: CurrentVersion, FPS: 30, Width: 1920, Height: 1080, Segments: []Segment{{
		ID: "a", Type: "clip", StartUS: 0, DurationUS: 1_000_000, SourceInUS: 110_000_000, ClipStartUS: 100_000_000,
		Crops: []CameraCrop{{ID: "ben", Name: "Ben", X: 0, Y: 0, Width: 0.5, Height: 1}},
		Shots: []CameraShot{{CropID: "ben", Start: 0, End: 10}},
	}}}
	s, err := BuildRenderSnapshot(d, RenderOptions{Format: "mp4", Quality: "high", CaptionMode: "none", Scope: "all"})
	if err != nil {
		t.Fatal(err)
	}
	d.Segments[0].Crops[0].X = 0.9
	d.Segments[0].Shots[0].End = 99
	d.Segments[0].ClipStartUS = 1
	r := s.Resolved[0]
	if len(r.Crops) != 1 || r.Crops[0].X != 0 || len(r.Shots) != 1 || r.Shots[0].End != 10 || r.ClipStartUS != 100_000_000 {
		t.Fatalf("multicam was not frozen: %#v", r)
	}
}
