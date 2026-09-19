package stitch

import (
	"encoding/json"
	"math"
	"testing"
)

func baseDoc() Document {
	return Document{Version: 1, FPS: 30, Width: 1920, Height: 1080, Segments: []Segment{{ID: "a", Type: "clip", StartUS: 0, DurationUS: 10_000_000}, {ID: "b", Type: "clip", StartUS: 10_000_000, DurationUS: 5_000_000}}, Captions: []Caption{{ID: "c", SegmentID: "a", Text: "caption", StartUS: 1_000_000, EndUS: 2_000_000}}, Overlays: []Overlay{{ID: "o", Kind: "text", StartUS: 0, EndUS: 5_000_000, Visible: true, Opacity: 1}}}
}
func TestTimingMoveMovesLinkedMembers(t *testing.T) {
	d := baseDoc()
	d.TimingLinks = []Group{{ID: "g", Members: []string{"a", "b", "c"}}}
	n, _, e := Apply(d, []Operation{{Type: "move_segment", TargetID: "a", DeltaUS: 1_000_000}})
	if e != nil || n.Segments[1].StartUS != 11_000_000 || n.Captions[0].StartUS != 2_000_000 {
		t.Fatalf("move: %#v %v", n, e)
	}
}
func TestTrimDoesNotDeleteCaptions(t *testing.T) {
	d := baseDoc()
	n, _, e := Apply(d, []Operation{{Type: "trim_segment", TargetID: "a", StartUS: 2_000_000, EndUS: 7_000_000}})
	if e != nil || len(n.Captions) != 1 || n.Segments[0].SourceInUS != 2_000_000 {
		t.Fatalf("trim: %#v %v", n, e)
	}
}
func TestApplyAtomicOnInvalid(t *testing.T) {
	d := baseDoc()
	n, _, e := Apply(d, []Operation{{Type: "set_title", Title: "changed"}, {Type: "move_segment", TargetID: "missing", DeltaUS: 1}})
	if e == nil || n.Title != "" {
		t.Fatalf("not atomic: %#v %v", n, e)
	}
}
func TestLegacyRoundTripAndSnap(t *testing.T) {
	d := baseDoc()
	raw, filters, e := ToLegacy(d)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = FromLegacy("x", "mp4", "high", raw, filters); e != nil {
		t.Fatal(e)
	}
	if SnapFrame(500_000, 30) != 500_000 {
		t.Fatalf("snap")
	}
	var v []any
	if json.Unmarshal(raw, &v) != nil {
		t.Fatal("bad json")
	}
}

func TestLegacyEncoderTimingAndPreservation(t *testing.T) {
	raw := json.RawMessage(`[{"id":"a","type":"clip","video_id":"v","start_ts":"100","end_ts":"110","duration":"10","title":"A","gain_db":-2,"filters":[{"name":"look"}],"transition":null},{"id":"b","type":"clip","video_id":"v","start_ts":200,"end_ts":205,"duration":5,"title":"B","transition":{"type":"crossfade","duration":1,"curve":"smooth"}}]`)
	d, err := FromLegacy("p", "mp4", "high", raw, json.RawMessage(`[{"name":"crop"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if d.Segments[0].StartUS != 0 || d.Segments[0].SourceInUS != 100e6 || d.Segments[0].DurationUS != 10e6 {
		t.Fatalf("first timing: %#v", d.Segments[0])
	}
	if d.Segments[1].StartUS != 9e6 || d.Segments[1].SourceInUS != 200e6 || d.Segments[1].Transition.DurationUS != 1e6 {
		t.Fatalf("second timing: %#v", d.Segments[1])
	}
	out, _, err := ToLegacy(d)
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]json.RawMessage
	if json.Unmarshal(out, &got) != nil {
		t.Fatal("invalid output")
	}
	var title string
	_ = json.Unmarshal(got[0]["title"], &title)
	if title != "A" || len(got[0]["filters"]) == 0 {
		t.Fatalf("lost preserved fields: %s", out)
	}
	var tr map[string]json.RawMessage
	_ = json.Unmarshal(got[1]["transition"], &tr)
	if string(tr["curve"]) != "\"smooth\"" {
		t.Fatalf("transition extension lost: %s", out)
	}
}

func TestFromLegacyTitleCardKeepsGothicText(t *testing.T) {
	raw := json.RawMessage(`[{"type":"title","text":"Chapter 1: Staying Positive","subtitle":"Ben Avery · September 3, 2026","duration":4,"bg_color":"#000000","text_color":"#f5f0e6","font_size":56,"position":"center"}]`)
	d, err := FromLegacy("Chapter 1: Staying Positive", "mp4", "high", raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Segments) != 1 || d.Segments[0].Type != "title" || d.Segments[0].Text != "Chapter 1: Staying Positive" || d.Segments[0].DurationUS != 4e6 {
		t.Fatalf("title card: %#v", d.Segments)
	}
	var legacy map[string]any
	if json.Unmarshal(d.Segments[0].Legacy, &legacy) != nil || legacy["subtitle"] != "Ben Avery · September 3, 2026" {
		t.Fatalf("legacy payload: %s", d.Segments[0].Legacy)
	}
	out, _, err := ToLegacy(d)
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if json.Unmarshal(out, &got) != nil || got[0]["text"] != "Chapter 1: Staying Positive" {
		t.Fatalf("round trip lost title text: %s", out)
	}
}

func TestLegacyTransitionCannotExceedPreviousSegment(t *testing.T) {
	raw := json.RawMessage(`[{"id":"a","start_ts":0,"duration":1},{"id":"b","start_ts":2,"duration":1,"transition":{"type":"crossfade","duration":2}}]`)
	if _, e := FromLegacy("", "", "", raw, nil); e == nil {
		t.Fatal("expected overlap error")
	}
}

func TestValidateBoundedReferencesAndNumbers(t *testing.T) {
	d := baseDoc()
	d.TimingLinks = []Group{{ID: "g", Members: []string{"a", "c", "o"}}}
	if Validate(d) != nil {
		t.Fatal("mixed timing group should be valid")
	}
	d.Captions[0].SegmentID = "c"
	if Validate(d) == nil {
		t.Fatal("caption self reference accepted")
	}
	d = baseDoc()
	d.Overlays[0].Opacity = math.NaN()
	if Validate(d) == nil {
		t.Fatal("nan opacity accepted")
	}
}
func TestValidatePrimaryOverlapAndBounds(t *testing.T) {
	d := baseDoc()
	d.Segments = []Segment{{ID: "a", Type: "clip", DurationUS: 10, StartUS: 0}, {ID: "b", Type: "clip", DurationUS: 10, StartUS: 5, Transition: &Transition{Kind: "x", DurationUS: 5}}, {ID: "c", Type: "clip", DurationUS: 10, StartUS: 6, Transition: &Transition{Kind: "x", DurationUS: 9}}}
	if Validate(d) == nil {
		t.Fatal("triple overlap accepted")
	}
	d = baseDoc()
	d.Segments[0].StartUS = maxDocumentUS
	d.Segments[0].DurationUS = 1
	if Validate(d) == nil {
		t.Fatal("time bound accepted")
	}
	d = baseDoc()
	d.Width = 0
	if Validate(d) == nil {
		t.Fatal("zero dimensions accepted")
	}
}

func TestReorderDuplicateTrimTimeline(t *testing.T) {
	d := baseDoc()
	n, _, e := Apply(d, []Operation{{Type: "reorder_segments", TargetID: "b", BeforeID: "a"}})
	if e != nil || n.Segments[0].ID != "b" || n.Segments[0].StartUS != 0 {
		t.Fatalf("reorder: %#v %v", n, e)
	}
	n, _, e = Apply(d, []Operation{{Type: "duplicate_segment", TargetID: "a"}})
	if e != nil || len(n.Segments) != 3 || n.Segments[1].StartUS != 10_000_000 {
		t.Fatalf("duplicate: %#v %v", n, e)
	}
	n, _, e = Apply(d, []Operation{{Type: "trim_segment", TargetID: "a", StartUS: 2_000_000, EndUS: 7_000_000}})
	if e != nil || n.Segments[0].StartUS != 2_000_000 || n.Segments[1].StartUS != 10_000_000 {
		t.Fatalf("trim: %#v %v", n, e)
	}
}

func TestDuplicateRipplesExplicitTimingGroup(t *testing.T) {
	d := Document{Version: 1, FPS: 30, Width: 1920, Height: 1080, Segments: []Segment{{ID: "a", Type: "clip", StartUS: 0, DurationUS: 10e6}, {ID: "b", Type: "clip", StartUS: 10e6, DurationUS: 5e6}}, Captions: []Caption{{ID: "cb", SegmentID: "b", Text: "b", StartUS: 11e6, EndUS: 12e6, Words: []Word{{Text: "b", StartUS: 11200000, EndUS: 11600000}}}}, Overlays: []Overlay{{ID: "ob", Kind: "text", StartUS: 11e6, EndUS: 13e6, Opacity: 1}}, TimingLinks: []Group{{ID: "g", Members: []string{"b", "cb", "ob"}}}}
	n, ids, e := Apply(d, []Operation{{Type: "duplicate_segment", TargetID: "a"}})
	if e != nil || n.Segments[2].StartUS != 20e6 || n.Captions[0].StartUS != 21e6 || n.Captions[0].Words[0].StartUS != 21200000 || n.Overlays[0].StartUS != 21e6 || len(ids) < 4 {
		t.Fatalf("ripple: %#v %v %v", n, ids, e)
	}
}
func TestReorderTimingGroupConflictAtomic(t *testing.T) {
	d := baseDoc()
	d.TimingLinks = []Group{{ID: "g", Members: []string{"a", "b"}}}
	n, _, e := Apply(d, []Operation{{Type: "reorder_segments", TargetID: "b", BeforeID: "a"}})
	if e == nil || n.Segments[0].ID != "a" {
		t.Fatalf("conflict not atomic: %#v %v", n, e)
	}
}
func TestRemoveSegmentRemovesAttachedCue(t *testing.T) {
	d := baseDoc()
	d.TimingLinks = []Group{{ID: "g", Members: []string{"a", "c"}}}
	n, _, e := Apply(d, []Operation{{Type: "remove_segment", TargetID: "a"}})
	if e != nil || len(n.Captions) != 0 || len(n.TimingLinks) != 0 {
		t.Fatalf("remove: %#v %v", n, e)
	}
}
func TestResolveSnapsAndAddsGaps(t *testing.T) {
	d := baseDoc()
	d.FPS = 29.97
	d.Segments[1].StartUS = 12_000_000
	r := Resolve(d)
	if len(r) != 3 || r[1].Segment.Type != "gap" {
		t.Fatalf("resolved gaps: %#v", r)
	}
}

func TestTeaserLayoutValidationAndOperations(t *testing.T) {
	d := baseDoc()
	layout := &TeaserLayout{Mode: TeaserLayoutTwoSpeakers, Crops: []TeaserCrop{{X: 0, Y: 0, Width: .5, Height: 1}, {X: .5, Y: 0, Width: .5, Height: 1}}}
	n, changed, err := Apply(d, []Operation{{Type: "set_canvas", Width: DefaultTeaserCanvasWidth, Height: DefaultTeaserCanvasHeight}, {Type: "set_segment_layout", TargetID: "a", Layout: layout}})
	if err != nil || n.Width != 1080 || n.Height != 1920 || n.Segments[0].Layout == nil || len(changed) != 2 {
		t.Fatalf("layout operation: %#v changed=%v err=%v", n, changed, err)
	}
	if _, _, err := Apply(d, []Operation{{Type: "set_canvas", Width: 1081, Height: 1920}}); err == nil {
		t.Fatal("accepted odd canvas width")
	}
	layout.Crops[0].X = .9
	if n.Segments[0].Layout.Crops[0].X != 0 {
		t.Fatal("operation retained caller-owned layout")
	}
	layout.Crops[0].X = 0
	bad := *layout
	bad.Mode = TeaserLayoutSingleSpeaker
	if _, _, err := Apply(d, []Operation{{Type: "set_segment_layout", TargetID: "a", Layout: &bad}}); err == nil {
		t.Fatal("accepted wrong crop count")
	}
	title := d
	title.Segments[0].Type = "title"
	if _, _, err := Apply(title, []Operation{{Type: "set_segment_layout", TargetID: "a", Layout: layout}}); err == nil {
		t.Fatal("accepted layout on title segment")
	}
}
func TestResolvedCaptionsClipsToSegment(t *testing.T) {
	d := baseDoc()
	d.Captions[0].StartUS = -1
	d.Captions[0].EndUS = 11_000_000
	c := ResolvedCaptions(d)
	if len(c) != 1 || c[0].StartUS != 0 || c[0].EndUS != 10_000_000 {
		t.Fatalf("caption clip: %#v", c)
	}
}
