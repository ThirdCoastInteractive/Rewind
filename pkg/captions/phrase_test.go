package captions

import "testing"

func TestReadableLinesJoinsWhisperWords(t *testing.T) {
	words := []struct {
		start, end float64
		text       string
	}{
		{0.00, 0.40, "Suppose"},
		{0.40, 0.70, "you"},
		{0.70, 0.90, "are"},
		{0.90, 1.20, "God."},
		{1.40, 1.80, "Suppose"},
		{1.80, 2.00, "you"},
		{2.00, 2.30, "have"},
		{2.30, 2.50, "all"},
		{2.50, 2.90, "time,"},
		{2.90, 3.20, "all"},
		{3.20, 3.80, "eternity,"},
		{3.80, 4.00, "and"},
		{4.00, 4.20, "all"},
		{4.20, 4.60, "power"},
		{4.60, 4.80, "at"},
		{4.80, 5.00, "your"},
		{5.00, 5.60, "disposal."},
		{6.20, 6.50, "What"},
		{6.50, 6.80, "would"},
		{6.80, 7.00, "you"},
		{7.00, 7.40, "do?"},
	}
	cues := make([]Cue, len(words))
	for i, w := range words {
		cues[i] = Cue{Start: w.start, End: w.end, Text: w.text}
	}
	got := ReadableLines(cues)
	want := []string{
		"Suppose you are God.",
		"Suppose you have all time, all eternity, and all power at your disposal.",
		"What would you do?",
	}
	if len(got) != len(want) {
		t.Fatalf("lines = %#v", got)
	}
	for i, line := range want {
		if got[i].Text != line {
			t.Errorf("line %d = %q, want %q", i, got[i].Text, line)
		}
	}
	if got[0].Start != 0 || got[0].End != 1.2 {
		t.Fatalf("first span %v-%v", got[0].Start, got[0].End)
	}
}

func TestReadableLinesLeavesPhrases(t *testing.T) {
	cues := []Cue{
		{Start: 0, End: 2, Text: "Windows are open"},
		{Start: 2, End: 4, Text: "Everybody step out"},
		{Start: 4, End: 6, Text: "The door is there"},
		{Start: 6, End: 8, Text: "Take the stairs"},
		{Start: 8, End: 10, Text: "Meet me outside"},
		{Start: 10, End: 12, Text: "Bring the keys"},
		{Start: 12, End: 14, Text: "Lock it behind you"},
		{Start: 14, End: 16, Text: "We leave now"},
	}
	got := ReadableLines(cues)
	if len(got) != len(cues) || got[0].Text != "Windows are open" {
		t.Fatalf("phrases changed: %#v", got)
	}
}
