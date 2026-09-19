package transcription

import (
	"testing"
	"thirdcoast.systems/rewind/pkg/captions"
)

func TestReplaceRangePreservesOutsideAndClipsCrossingCue(t *testing.T) {
	old := []captions.Cue{{Start: 0, End: 10, Text: "before"}, {Start: 10, End: 40, Text: "crossing"}, {Start: 40, End: 50, Text: "after"}}
	got := ReplaceRange(old, []captions.Cue{{Start: 20, End: 30, Text: "repaired"}}, 20, 30)
	if len(got) != 5 || got[1].Start != 10 || got[1].End != 20 || got[2].Text != "repaired" || got[3].Start != 30 || got[3].End != 40 || got[4].Text != "after" {
		t.Fatalf("incorrect splice: %+v", got)
	}
	if old[1].End != 40 {
		t.Fatal("modified original cues")
	}
}
