package stitch

import "testing"

func TestTrimSegmentPreservesGapPositionAndDropsOversizedTransition(t *testing.T) {
	d := Document{Version: CurrentVersion, FPS: 30, Width: 100, Height: 100, Segments: []Segment{{ID: "gap", Type: "gap", StartUS: 0, DurationUS: 1_000_000}, {ID: "clip", Type: "video", StartUS: 900_000, SourceInUS: 10_000_000, DurationUS: 5_000_000, Transition: &Transition{Kind: "fade", DurationUS: 100_000}}}}
	out, _, err := Apply(d, []Operation{{Type: "trim_segment", TargetID: "clip", StartUS: 1_000_000, EndUS: 1_050_000}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Segments[0].StartUS != 0 || out.Segments[0].DurationUS != 1_000_000 || out.Segments[1].StartUS != 1_900_000 || out.Segments[1].SourceInUS != 11_000_000 || out.Segments[1].Transition != nil {
		t.Fatalf("segments=%#v", out.Segments)
	}
}

func TestReorderRejectsExplicitTimingGroupConflict(t *testing.T) {
	d := Document{Version: CurrentVersion, FPS: 30, Width: 100, Height: 100, Segments: []Segment{{ID: "a", Type: "video", DurationUS: 1_000_000}, {ID: "b", Type: "video", StartUS: 1_000_000, DurationUS: 1_000_000}}, TimingLinks: []Group{{ID: "g", Members: []string{"a", "b"}}}}
	if _, _, err := Apply(d, []Operation{{Type: "reorder_segments", TargetID: "b", BeforeID: "a"}}); err == nil {
		t.Fatal("expected timing-linked reorder rejection")
	}
}

func TestGroupRangeTrimIntersectsProjectRangePerSegment(t *testing.T) {
	d := Document{Version: CurrentVersion, FPS: 30, Width: 100, Height: 100, Segments: []Segment{{ID: "a", Type: "video", StartUS: 0, SourceInUS: 10_000_000, DurationUS: 5_000_000}, {ID: "b", Type: "video", StartUS: 10_000_000, SourceInUS: 20_000_000, DurationUS: 5_000_000}, {ID: "other", Type: "gap", StartUS: 20_000_000, DurationUS: 2_000_000}}}
	out, _, err := Apply(d, []Operation{{Type: "group_range_trim", IDs: []string{"a", "b"}, StartUS: 3_000_000, EndUS: 12_000_000}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Segments[0].StartUS != 3_000_000 || out.Segments[0].SourceInUS != 13_000_000 || out.Segments[0].DurationUS != 2_000_000 || out.Segments[1].StartUS != 10_000_000 || out.Segments[1].SourceInUS != 20_000_000 || out.Segments[1].DurationUS != 2_000_000 || out.Segments[2].StartUS != 20_000_000 {
		t.Fatalf("segments=%#v", out.Segments)
	}
}

func TestGroupRangeTrimRejectsDisjointSelectionAtomically(t *testing.T) {
	d := Document{Version: CurrentVersion, FPS: 30, Width: 100, Height: 100, Segments: []Segment{{ID: "a", Type: "video", StartUS: 0, DurationUS: 1_000_000}, {ID: "b", Type: "video", StartUS: 10_000_000, DurationUS: 1_000_000}}}
	out, _, err := Apply(d, []Operation{{Type: "group_range_trim", IDs: []string{"a", "b"}, StartUS: 2_000_000, EndUS: 3_000_000}})
	if err == nil || out.Segments[0].DurationUS != 1_000_000 || out.Segments[1].DurationUS != 1_000_000 {
		t.Fatalf("expected atomic rejection out=%#v err=%v", out, err)
	}
}

func TestSplitSegmentRepairsTimingLinksForMovedAndCrossingCaptions(t *testing.T) {
	d := Document{Version: CurrentVersion, FPS: 30, Width: 100, Height: 100, Segments: []Segment{{ID: "s", Type: "video", DurationUS: 10_000_000}}, Captions: []Caption{{ID: "m", SegmentID: "s", Text: "m", StartUS: 7_000_000, EndUS: 8_000_000}, {ID: "x", SegmentID: "s", Text: "x", StartUS: 4_000_000, EndUS: 6_000_000}, {ID: "u", SegmentID: "s", Text: "u", StartUS: 7_000_000, EndUS: 8_000_000}}, TimingLinks: []Group{{ID: "g", Members: []string{"s", "m", "x"}}}}
	out, _, err := Apply(d, []Operation{{Type: "split_segment", TargetID: "s", DeltaUS: 5_000_000}})
	if err != nil {
		t.Fatal(err)
	}
	var moved, duplicate, unlinked string
	for _, c := range out.Captions {
		if c.Text == "m" {
			moved = c.SegmentID
		}
		if c.Text == "x" && c.SegmentID != "s" {
			duplicate = c.ID
		}
		if c.Text == "u" {
			unlinked = c.SegmentID
		}
	}
	if moved == "" || moved == "s" || duplicate == "" || unlinked != moved {
		t.Fatalf("captions=%#v", out.Captions)
	}
	var left, right bool
	for _, g := range out.TimingLinks {
		if g.ID == "g" {
			left = contains(g.Members, "s") && contains(g.Members, "x") && !contains(g.Members, "m") && !contains(g.Members, duplicate)
		} else if contains(g.Members, moved) && contains(g.Members, duplicate) {
			right = true
		}
	}
	if !left || !right {
		t.Fatalf("partitioned groups missing: %#v", out.TimingLinks)
	}
}

func TestTrimSegmentRestoresEarlierSourceAndPreservesCaptions(t *testing.T) {
	d := Document{Version: CurrentVersion, FPS: 30, Width: 100, Height: 100, Segments: []Segment{{ID: "s", Type: "video", StartUS: 2_000_000, SourceInUS: 2_000_000, DurationUS: 5_000_000}}, Captions: []Caption{{ID: "c", SegmentID: "s", Text: "cue", StartUS: 3_000_000, EndUS: 4_000_000, Words: []Word{{Text: "cue", StartUS: 3_200_000, EndUS: 3_500_000}}}}}
	out, _, err := Apply(d, []Operation{{Type: "trim_segment", TargetID: "s", StartUS: -1_000_000, EndUS: 6_000_000}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Segments[0].SourceInUS != 1_000_000 || out.Segments[0].StartUS != 1_000_000 || out.Segments[0].DurationUS != 7_000_000 || out.Captions[0].StartUS != 3_000_000 || out.Captions[0].Words[0].StartUS != 3_200_000 {
		t.Fatalf("trim=%#v captions=%#v", out.Segments, out.Captions)
	}
}

func TestTrimSegmentRejectsNegativeProjectStartAndEmptyRange(t *testing.T) {
	d := Document{Version: CurrentVersion, FPS: 30, Width: 100, Height: 100, Segments: []Segment{{ID: "s", Type: "video", StartUS: 0, SourceInUS: 1_000_000, DurationUS: 5_000_000}}}
	for _, op := range []Operation{{Type: "trim_segment", TargetID: "s", StartUS: -1, EndUS: 2_000_000}, {Type: "trim_segment", TargetID: "s", StartUS: 3_000_000, EndUS: 3_000_000}} {
		if _, _, err := Apply(d, []Operation{op}); err == nil {
			t.Fatalf("expected invalid trim: %#v", op)
		}
	}
}

func TestUpsertCaptionInvalidatesAlignmentOnlyForCueIdentityChanges(t *testing.T) {
	base := Caption{ID: "c", Text: "hello", Language: "en", StartUS: 0, EndUS: 2_000_000, Alignment: "valid", AlignmentKey: "key", Words: []Word{{Text: "hello", StartUS: 0, EndUS: 1_000_000}}}
	cases := []struct {
		name  string
		c     Caption
		valid bool
	}{
		{"style", Caption{ID: "c", Text: "hello", Language: "en", StartUS: 0, EndUS: 2_000_000, Alignment: "valid", AlignmentKey: "key", Words: base.Words, Style: CaptionStyle{Bold: true}}, true},
		{"word timing", Caption{ID: "c", Text: "hello", Language: "en", StartUS: 0, EndUS: 2_000_000, Alignment: "valid", AlignmentKey: "key", Words: []Word{{Text: "hello", StartUS: 500_000, EndUS: 1_500_000}}}, true},
		{"text", Caption{ID: "c", Text: "changed", Language: "en", StartUS: 0, EndUS: 2_000_000, Alignment: "valid", AlignmentKey: "key", Words: base.Words}, false},
		{"language", Caption{ID: "c", Text: "hello", Language: "fr", StartUS: 0, EndUS: 2_000_000, Alignment: "valid", AlignmentKey: "key", Words: base.Words}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := Document{Version: CurrentVersion, FPS: 30, Width: 100, Height: 100, Captions: []Caption{base}}
			out, _, err := Apply(d, []Operation{{Type: "upsert_caption", Caption: &tc.c}})
			if err != nil {
				t.Fatal(err)
			}
			if (out.Captions[0].Alignment == "valid") != tc.valid || (tc.valid && len(out.Captions[0].Words) == 0) || (!tc.valid && len(out.Captions[0].Words) != 0) {
				t.Fatalf("caption=%#v", out.Captions[0])
			}
		})
	}
}
