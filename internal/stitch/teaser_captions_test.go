package stitch

import (
	"strings"
	"testing"
)

func TestStyleTeaserCaptionsKeepsVerifiedWordBoundsAndSources(t *testing.T) {
	d := baseDoc()
	d.Width, d.Height = 1080, 1920
	d.Captions = []Caption{{
		ID: "cap", SegmentID: "a", Text: "one two three four five six", Language: "en",
		StartUS: 1_000_000, EndUS: 7_000_000, Words: []Word{
			{Text: "one", StartUS: 1_000_000, EndUS: 2_000_000}, {Text: "two", StartUS: 2_000_000, EndUS: 3_000_000},
			{Text: "three", StartUS: 3_000_000, EndUS: 4_000_000}, {Text: "four", StartUS: 4_000_000, EndUS: 5_000_000},
			{Text: "five", StartUS: 5_000_000, EndUS: 6_000_000}, {Text: "six", StartUS: 6_000_000, EndUS: 7_000_000},
		}, Alignment: "valid", SourceVideoID: "video", SourceStartUS: 11_000_000, SourceEndUS: 17_000_000,
	}}
	r, err := StyleTeaserCaptions(d, TeaserCaptionOptions{Style: "bold", MaxCharsPerLine: 10, MaxLines: 1, MaxPhraseChars: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Captions) < 3 || len(r.DeleteIDs) != 1 || r.DeleteIDs[0] != "cap" {
		t.Fatalf("expected deterministic phrase split: %#v", r)
	}
	for _, c := range r.Captions {
		if c.Alignment != "valid" || len(c.Words) == 0 || c.StartUS < 1_000_000 || c.EndUS > 7_000_000 {
			t.Fatalf("invalid phrase timing: %#v", c)
		}
		for _, w := range c.Words {
			if w.StartUS < c.StartUS || w.EndUS > c.EndUS || w.EndUS <= w.StartUS {
				t.Fatalf("word escaped phrase: %#v", c)
			}
		}
		if c.SourceVideoID != "video" || c.SourceStartUS < 11_000_000 || c.SourceEndUS > 17_000_000 {
			t.Fatalf("source mapping lost: %#v", c)
		}
		for _, line := range strings.Split(c.Text, "\n") {
			if len([]rune(line)) > 10 {
				t.Fatalf("line too long: %q", line)
			}
		}
	}
}

func TestStyleTeaserCaptionsUnalignedDoesNotInventWords(t *testing.T) {
	d := baseDoc()
	d.Width, d.Height = 1080, 1920
	d.Captions = []Caption{{ID: "cap", SegmentID: "a", Text: "これは非常に長い字幕テキストです", StartUS: 1, EndUS: 2, Alignment: "unaligned", SourceVideoID: "v", SourceStartUS: 10, SourceEndUS: 20}}
	r, err := StyleTeaserCaptions(d, TeaserCaptionOptions{Style: "karaoke", MaxCharsPerLine: 4, MaxLines: 2})
	if err != nil {
		t.Fatal(err)
	}
	c := r.Captions[0]
	if c.Words != nil || c.Alignment != "unaligned" || c.StartUS != 1 || c.EndUS != 2 {
		t.Fatalf("unaligned caption changed timing/alignment: %#v", c)
	}
	if c.Style.WordHighlight {
		t.Fatal("karaoke invented word highlighting")
	}
	if len(r.Warnings) < 2 || !strings.Contains(strings.Join(r.Warnings, " "), "needs alignment") {
		t.Fatalf("missing alignment warning: %#v", r.Warnings)
	}
	for _, line := range strings.Split(c.Text, "\n") {
		if len([]rune(line)) > 4 {
			t.Fatalf("non-Latin line too long: %q", line)
		}
	}
}

func TestStyleTeaserCaptionsUsesPortraitPixelPlacement(t *testing.T) {
	d := baseDoc()
	d.Width, d.Height = 1080, 1920
	d.Captions = []Caption{{ID: "cap", SegmentID: "a", Text: "hello", StartUS: 1, EndUS: 2, Alignment: "unaligned"}}
	r, err := StyleTeaserCaptions(d, TeaserCaptionOptions{Margins: TeaserCaptionMargins{Left: .1, Right: .1, Top: .05, Bottom: .15}})
	if err != nil {
		t.Fatal(err)
	}
	s := r.Captions[0].Style
	if s.FontSize < 24 || s.X != 540 || s.Y != 1632 || !s.SafeArea {
		t.Fatalf("unexpected 9:16 style placement: %#v", s)
	}
}

func TestStyleTeaserCaptionsKaraokeUsesSingleLinePhrases(t *testing.T) {
	d := baseDoc()
	d.Width, d.Height = 1080, 1920
	d.Captions = []Caption{{ID: "cap", SegmentID: "a", Text: "one two three four", StartUS: 1_000_000, EndUS: 5_000_000, Alignment: "valid", Words: []Word{
		{Text: "one", StartUS: 1_000_000, EndUS: 2_000_000}, {Text: "two", StartUS: 2_000_000, EndUS: 3_000_000},
		{Text: "three", StartUS: 3_000_000, EndUS: 4_000_000}, {Text: "four", StartUS: 4_000_000, EndUS: 5_000_000},
	}}}
	r, err := StyleTeaserCaptions(d, TeaserCaptionOptions{Style: "karaoke", MaxCharsPerLine: 8, MaxLines: 2, MaxPhraseChars: 32})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Captions) != 3 {
		t.Fatalf("karaoke ignored single-line grouping: %#v", r.Captions)
	}
	for _, c := range r.Captions {
		if !c.Style.WordHighlight || strings.Contains(c.Text, "\n") {
			t.Fatalf("karaoke phrase rendered across lines: %#v", c)
		}
		if len([]rune(c.Text)) > 8 {
			t.Fatalf("karaoke phrase exceeds line bound: %q", c.Text)
		}
	}
}

func TestTeaserCaptionOperationsAreStable(t *testing.T) {
	d := baseDoc()
	d.Width, d.Height = 1080, 1920
	d.Captions = []Caption{{ID: "cap", SegmentID: "a", Text: "hello", StartUS: 1, EndUS: 2}}
	r, err := StyleTeaserCaptions(d, TeaserCaptionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ops := TeaserCaptionOperations(r)
	if len(ops) != 1 || ops[0].Type != "upsert_caption" || ops[0].Caption.ID != "cap" {
		t.Fatalf("unexpected operations: %#v", ops)
	}
}

func TestTeaserCaptionOperationsPreserveTimingLinksWhenSplitting(t *testing.T) {
	d := baseDoc()
	d.Width, d.Height = 1080, 1920
	d.Captions = []Caption{{ID: "cap", SegmentID: "a", Text: "one two three four", StartUS: 1_000_000, EndUS: 5_000_000, Alignment: "valid", SourceVideoID: "video", SourceStartUS: 1_000_000, SourceEndUS: 5_000_000, Words: []Word{{Text: "one", StartUS: 1_000_000, EndUS: 2_000_000}, {Text: "two", StartUS: 2_000_000, EndUS: 3_000_000}, {Text: "three", StartUS: 3_000_000, EndUS: 4_000_000}, {Text: "four", StartUS: 4_000_000, EndUS: 5_000_000}}}}
	d.TimingLinks = []Group{{ID: "link", Members: []string{"cap", "b"}}}
	r, err := StyleTeaserCaptions(d, TeaserCaptionOptions{MaxCharsPerLine: 8, MaxLines: 1, MaxPhraseChars: 8})
	if err != nil {
		t.Fatal(err)
	}
	n, _, err := Apply(d, TeaserCaptionOperationsForDocument(d, r))
	if err != nil {
		t.Fatal(err)
	}
	if len(n.TimingLinks) != 1 || !contains(n.TimingLinks[0].Members, "b") || len(n.TimingLinks[0].Members) != len(r.Captions)+1 {
		t.Fatalf("timing link was not rebuilt: %#v", n.TimingLinks)
	}
}

func TestSelectVerifiedImportWordsClipsBoundaryWords(t *testing.T) {
	c := sourceImportCue{Start: 1, End: 5, Text: "one two three four", Words: []sourceImportWord{
		{Text: "one", Start: f64ptr(1), End: f64ptr(2)},
		{Text: "two", Start: f64ptr(2), End: f64ptr(3)},
		{Text: "three", Start: f64ptr(3), End: f64ptr(4)},
		{Text: "four", Start: f64ptr(4), End: f64ptr(5)},
	}}
	words, ok := selectVerifiedImportWords(c, 2_000_000, 4_000_000)
	if !ok || len(words) != 2 || words[0].Text != "two" || words[1].Text != "three" {
		t.Fatalf("boundary words were not filtered: ok=%v words=%#v", ok, words)
	}
	if *words[0].Start != 2 || *words[1].End != 4 {
		t.Fatalf("selected word timing was clipped or changed: %#v", words)
	}
}

func TestVerifiedCaptionWordsRejectsTextMismatch(t *testing.T) {
	d := baseDoc()
	d.Width, d.Height = 1080, 1920
	d.Captions = []Caption{{ID: "cap", SegmentID: "a", Text: "one two", StartUS: 1, EndUS: 3, Alignment: "valid", Words: []Word{
		{Text: "one", StartUS: 1, EndUS: 2}, {Text: "different", StartUS: 2, EndUS: 3},
	}}}
	if verifiedCaptionWords(d.Captions[0]) {
		t.Fatal("mismatched word text was accepted as verified")
	}
	r, err := StyleTeaserCaptions(d, TeaserCaptionOptions{Style: "karaoke"})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Captions) != 1 || r.Captions[0].Words != nil || r.Captions[0].Alignment != "unaligned" || r.Captions[0].Text != "one two" {
		t.Fatalf("mismatched words were used for aligned output: %#v", r.Captions)
	}
}

func f64ptr(v float64) *float64 { return &v }
