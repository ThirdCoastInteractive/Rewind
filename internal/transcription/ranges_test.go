package transcription

import (
	"testing"
	"thirdcoast.systems/rewind/pkg/captions"
)

func TestMergeRangeUsesAbsoluteTimeAndPreservesOtherRanges(t *testing.T) {
	old := []captions.Cue{{Start: 10, End: 12, Text: "keep"}, {Start: 101, End: 104, Text: "replace"}}
	got := MergeRange(old, []captions.Cue{{Start: 1, End: 4, Text: "new"}}, 100, 110)
	if len(got) != 2 || got[0].Text != "keep" || got[1].Start != 101 || got[1].Text != "new" {
		t.Fatalf("bad merge: %+v", got)
	}
}
