package mlcore

import "testing"

func TestReconcileNoGapsNoOverlaps(t *testing.T) {
	in := []Window{
		{Start: 2, End: 10, Title: "intro"},
		{Start: 12, End: 20, Title: "middle"},
		{Start: 18, End: 25, Title: "overlap-tail"},
	}
	out := Reconcile(in, 30)
	if err := WindowsCovered(out, 30); err != nil {
		t.Fatal(err)
	}
	if out[0].Start != 0 {
		t.Fatalf("expected coverage from 0, got %v", out[0].Start)
	}
	if out[len(out)-1].End != 30 {
		t.Fatalf("expected coverage to 30, got %v", out[len(out)-1].End)
	}
}

func TestReconcileHourLongSingleWindowAllowed(t *testing.T) {
	in := []Window{{Start: 0, End: 3720, Title: "the whole show"}}
	out := Reconcile(in, 3720)
	if len(out) != 1 {
		t.Fatalf("want 1 window, got %d", len(out))
	}
	if out[0].Duration() < 3600 {
		t.Fatalf("hour-long window not preserved: %v", out[0].Duration())
	}
	if err := WindowsCovered(out, 3720); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileEmptyMakesFullCoverage(t *testing.T) {
	out := Reconcile(nil, 12)
	if len(out) != 1 || out[0].Start != 0 || out[0].End != 12 {
		t.Fatalf("%+v", out)
	}
}

func TestApplyOverridesSameHash(t *testing.T) {
	gen := []Window{{Start: 0, End: 10, Title: "generated", Summary: "g"}}
	prev := []Window{{
		Start: 1, End: 9, Title: "human title", Summary: "human summary",
		OverrideTitle: true, OverrideSummary: true,
	}}
	out := ApplyOverrides(gen, prev, true)
	if out[0].Title != "human title" || !out[0].OverrideTitle {
		t.Fatalf("title override lost: %+v", out[0])
	}
	if out[0].Summary != "human summary" || !out[0].OverrideSummary {
		t.Fatalf("summary override lost: %+v", out[0])
	}
}

func TestSnapToCues(t *testing.T) {
	cues := []Cue{
		{ID: 1, Start: 0, End: 1.2},
		{ID: 2, Start: 1.2, End: 3.0},
		{ID: 3, Start: 3.0, End: 5.5},
	}
	w := SnapToCues(Window{Start: 0.4, End: 3.4}, cues)
	if w.Start != 0 || w.End != 3.0 {
		t.Fatalf("snapped=%v-%v", w.Start, w.End)
	}
	kept := SnapToCues(Window{Start: 1, End: 4, OverrideBounds: true}, cues)
	if kept.Start != 1 || kept.End != 4 {
		t.Fatalf("override bounds must not snap: %+v", kept)
	}
}

func TestAttachShortsNestsUnderParent(t *testing.T) {
	parents := []Window{
		{Start: 0, End: 600, Title: "A"},
		{Start: 600, End: 1200, Title: "B"},
	}
	shorts := []Window{
		{Start: 10, End: 28, Title: "early"},
		{Start: 650, End: 668, Title: "later"},
		{Start: 20, End: 200, Title: "too long"},
		{Start: 12, End: 16, Title: "too short"},
	}
	out := AttachShorts(parents, shorts)
	if len(out[0].Shorts) != 1 || out[0].Shorts[0].Title != "early" {
		t.Fatalf("parent A shorts: %+v", out[0].Shorts)
	}
	if len(out[1].Shorts) != 1 || out[1].Shorts[0].Title != "later" {
		t.Fatalf("parent B shorts: %+v", out[1].Shorts)
	}
}

func TestFlattenGeneratedPullsNestedShorts(t *testing.T) {
	in := []Window{{
		Start: 0, End: 100, Title: "ch",
		Shorts: []Window{{Start: 10, End: 25, Title: "s"}},
	}}
	parents, shorts := FlattenGenerated(in)
	if len(parents) != 1 || parents[0].Title != "ch" || len(parents[0].Shorts) != 0 {
		t.Fatalf("parents %+v", parents)
	}
	if len(shorts) != 1 || shorts[0].Title != "s" {
		t.Fatalf("shorts %+v", shorts)
	}
}

func TestApplyOverridesHashChangedLeavesGeneratedWindows(t *testing.T) {
	gen := []Window{{Start: 0, End: 10, Title: "generated"}}
	prev := []Window{{Start: 0, End: 10, Title: "human", OverrideTitle: true}}
	out := ApplyOverrides(gen, prev, false)
	if out[0].Title != "generated" || out[0].OverrideTitle {
		t.Fatalf("hash change must not paste old titles onto new windows: %+v", out[0])
	}
}
