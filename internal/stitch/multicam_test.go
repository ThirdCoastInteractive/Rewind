package stitch

import "testing"

func TestSetSegmentMulticamApplyCloneValidate(t *testing.T) {
	d := baseDoc()
	crops := []CameraCrop{
		{ID: "ben", Name: "Ben", AspectRatio: "9:16", X: 0, Y: 0, Width: 0.5, Height: 1},
		{ID: "izzy", Name: "Izzy", AspectRatio: "9:16", X: 0.5, Y: 0, Width: 0.5, Height: 1},
	}
	shots := []CameraShot{
		{CropID: "ben", Start: 0, End: 5, TransitionOut: &CameraShotTransition{Type: "fade", Duration: 0.5}},
		{CropID: "izzy", Start: 5, End: 12},
	}
	n, changed, err := Apply(d, []Operation{{Type: "set_segment_multicam", TargetID: "a", Crops: crops, Shots: shots}})
	if err != nil || len(changed) != 1 || changed[0] != "a" {
		t.Fatalf("apply: changed=%v err=%v", changed, err)
	}
	if len(n.Segments[0].Crops) != 2 || len(n.Segments[0].Shots) != 2 {
		t.Fatalf("multicam not set: %#v", n.Segments[0])
	}
	crops[0].X = 0.9
	shots[0].Start = 99
	if n.Segments[0].Crops[0].X != 0 || n.Segments[0].Shots[0].Start != 0 {
		t.Fatal("operation retained caller-owned multicam slices")
	}
	n2, _, err := Apply(n, []Operation{{Type: "set_segment_multicam", TargetID: "a", Crops: nil, Shots: nil}})
	if err != nil || len(n2.Segments[0].Crops) != 0 || len(n2.Segments[0].Shots) != 0 {
		t.Fatalf("clear failed: %#v err=%v", n2.Segments[0], err)
	}
}

func TestSetSegmentMulticamRejectsInvalidAtomically(t *testing.T) {
	d := baseDoc()
	d.Title = "keep"
	crops := []CameraCrop{{ID: "ben", X: 0, Y: 0, Width: 0.5, Height: 1}}
	badOverlap := []CameraShot{
		{CropID: "ben", Start: 0, End: 5},
		{CropID: "ben", Start: 4, End: 8},
	}
	n, _, err := Apply(d, []Operation{
		{Type: "set_title", Title: "changed"},
		{Type: "set_segment_multicam", TargetID: "a", Crops: crops, Shots: badOverlap},
	})
	if err == nil || n.Title != "keep" || len(n.Segments[0].Crops) != 0 {
		t.Fatalf("not atomic on overlap: %#v err=%v", n, err)
	}
	badCrop := []CameraCrop{{ID: "x", X: 0.8, Y: 0, Width: 0.5, Height: 1}}
	if _, _, err := Apply(d, []Operation{{Type: "set_segment_multicam", TargetID: "a", Crops: badCrop}}); err == nil {
		t.Fatal("accepted invalid crop rect")
	}
	if _, _, err := Apply(d, []Operation{{Type: "set_segment_multicam", TargetID: "a", Crops: nil, Shots: []CameraShot{{CropID: "missing", Start: 0, End: 1}}}}); err == nil {
		t.Fatal("accepted shots without crops")
	}
	dup := []CameraCrop{{ID: "a", X: 0, Y: 0, Width: 0.4, Height: 1}, {ID: "a", X: 0.5, Y: 0, Width: 0.4, Height: 1}}
	if _, _, err := Apply(d, []Operation{{Type: "set_segment_multicam", TargetID: "a", Crops: dup}}); err == nil {
		t.Fatal("accepted duplicate crop ids")
	}
	longTr := []CameraShot{{CropID: "ben", Start: 0, End: 1, TransitionOut: &CameraShotTransition{Type: "fade", Duration: 1}}}
	if _, _, err := Apply(d, []Operation{{Type: "set_segment_multicam", TargetID: "a", Crops: crops, Shots: longTr}}); err == nil {
		t.Fatal("accepted transition >= shot duration")
	}
}

func TestSetSegmentMulticamRejectsTitle(t *testing.T) {
	d := baseDoc()
	d.Segments[0].Type = "title"
	crops := []CameraCrop{{ID: "ben", X: 0, Y: 0, Width: 0.5, Height: 1}}
	if _, _, err := Apply(d, []Operation{{Type: "set_segment_multicam", TargetID: "a", Crops: crops}}); err == nil {
		t.Fatal("accepted multicam on title segment")
	}
	d.Segments[0].Type = "clip"
	d.Segments[0].Crops = crops
	d.Segments[0].Type = "gap"
	if Validate(d) == nil {
		t.Fatal("Validate accepted multicam on gap")
	}
}

func TestVisibleShotsTrimRebase(t *testing.T) {
	shots := []CameraShot{
		{CropID: "ben", Start: 5, End: 25, TransitionOut: &CameraShotTransition{Type: "fade", Duration: 2}},
		{CropID: "izzy", Start: 25, End: 40},
		{CropID: "ben", Start: 0, End: 2},
	}
	// clipStart=100s, sourceIn=110s, duration=20s → window clip-rel 10–30
	got := VisibleShots(shots, 100_000_000, 110_000_000, 20_000_000)
	if len(got) != 2 {
		t.Fatalf("expected 2 visible shots, got %#v", got)
	}
	if got[0].CropID != "ben" || got[0].Start != 0 || got[0].End != 15 {
		t.Fatalf("first shot: %#v", got[0])
	}
	if got[0].TransitionOut == nil || got[0].TransitionOut.Duration != 2 {
		t.Fatalf("transition: %#v", got[0].TransitionOut)
	}
	if got[1].CropID != "izzy" || got[1].Start != 15 || got[1].End != 20 {
		t.Fatalf("second shot: %#v", got[1])
	}
	// Transition longer than trimmed shot is clamped.
	tight := VisibleShots([]CameraShot{{CropID: "ben", Start: 0, End: 12, TransitionOut: &CameraShotTransition{Type: "fade", Duration: 5}}}, 0, 10_000_000, 1_000_000)
	if len(tight) != 1 || tight[0].Start != 0 || tight[0].End != 1 {
		t.Fatalf("tight window: %#v", tight)
	}
	if tight[0].TransitionOut == nil || tight[0].TransitionOut.Duration != 0.5 {
		t.Fatalf("clamped transition: %#v", tight[0].TransitionOut)
	}
}
