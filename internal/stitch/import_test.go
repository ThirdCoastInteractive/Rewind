package stitch

import "testing"

func TestImportCaptionsBoundedAndDeduplicated(t *testing.T) {
	d := baseDoc()
	caps := make([]Caption, 101)
	for i := range caps {
		caps[i] = Caption{SegmentID: "a", Text: "cue", StartUS: int64(i) * 100000, EndUS: int64(i+1) * 100000, Language: "en", SourceVideoID: "video", SourceStartUS: int64(i) * 100000, SourceEndUS: int64(i+1) * 100000}
	}
	n, _, e := Apply(d, []Operation{{Type: "import_captions", TargetID: "a", Language: "en", Captions: caps}})
	if e != nil || len(n.Captions) != 102 {
		t.Fatalf("import: %v %d", e, len(n.Captions))
	}
	n, _, e = Apply(n, []Operation{{Type: "import_captions", TargetID: "a", Language: "en", Captions: caps}})
	if e != nil || len(n.Captions) != 102 {
		t.Fatalf("dedupe: %v %d", e, len(n.Captions))
	}
}
func TestImportCaptionsExtendsExistingTimingGroup(t *testing.T) {
	d := baseDoc()
	d.TimingLinks = []Group{{ID: "g", Members: []string{"a", "b"}}}
	c := Caption{SegmentID: "a", Text: "x", StartUS: 1, EndUS: 2, Language: "en", SourceVideoID: "v", SourceStartUS: 1, SourceEndUS: 2}
	n, _, e := Apply(d, []Operation{{Type: "import_captions", TargetID: "a", Captions: []Caption{c}}})
	if e != nil || len(n.TimingLinks) != 1 || !contains(n.TimingLinks[0].Members, n.Captions[1].ID) {
		t.Fatalf("group: %#v %v", n, e)
	}
}
