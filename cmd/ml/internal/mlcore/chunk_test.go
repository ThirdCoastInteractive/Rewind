package mlcore

import (
	"strings"
	"testing"

	"thirdcoast.systems/rewind/pkg/captions"
)

func TestChunkShortCues(t *testing.T) {
	cues := CuesFromCaptions([]captions.Cue{
		{Start: 0, End: 1.5, Text: "Hello there."},
		{Start: 1.5, End: 3.0, Text: "This is a short clip."},
		{Start: 3.0, End: 4.2, Text: "Goodbye."},
	})
	if len(cues) != 3 || cues[0].ID != 1 || cues[2].ID != 3 {
		t.Fatalf("ids: %+v", cues)
	}
	chunks := ChunkCues(cues, 24000, 2000)
	if len(chunks) != 1 {
		t.Fatalf("short cues should fit in one chunk, got %d", len(chunks))
	}
	if !strings.Contains(chunks[0].Text, "[c0001") || !strings.Contains(chunks[0].Text, "[c0003") {
		t.Fatalf("missing cue IDs: %s", chunks[0].Text)
	}
}

func TestCueByIDAcceptsChunkRelativeIndexes(t *testing.T) {
	cues := []Cue{
		{ID: 500, Start: 10, End: 12, Text: "a"},
		{ID: 501, Start: 12, End: 14, Text: "b"},
	}
	c, ok := CueByID(cues, 500)
	if !ok || c.Text != "a" {
		t.Fatalf("global id: %+v %v", c, ok)
	}
	c, ok = CueByID(cues, 1)
	if !ok || c.Text != "a" {
		t.Fatalf("relative id: %+v %v", c, ok)
	}
	got := WindowsFromModel([]ModelWindow{{Title: "scene", CueStart: 1, CueEnd: 2}}, cues)
	if len(got) != 1 || got[0].Start != 10 || got[0].End != 14 {
		t.Fatalf("mapped %+v", got)
	}
}

func TestChunkNoisyAutoCaption(t *testing.T) {
	// YouTube-ish rolling duplicates and junk casing.
	raw := []captions.Cue{
		{Start: 0, End: 2, Text: "um so like TODAY we are going to"},
		{Start: 1.2, End: 3.5, Text: "so like TODAY we are going to talk"},
		{Start: 3.0, End: 5.5, Text: "[Music]  talk about the  thing"},
		{Start: 5.0, End: 8, Text: "talk about the thing   okay"},
	}
	cues := CuesFromCaptions(raw)
	chunks := ChunkCues(cues, 64, 16)
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}
	joined := chunks[0].Text
	if !strings.Contains(joined, "TODAY") {
		t.Fatalf("noisy text dropped: %s", joined)
	}
}

func TestTrimUserToFitMakesOversizeRepairFit(t *testing.T) {
	system := RepairSystemPrompt()
	prev := strings.Repeat("{\"title\":\"x\",\"cue_start\":1,\"cue_end\":2},", 4000)
	user := trimUserToFit(system, "Previous reply:\n"+prev)
	if !chatFits(system, user) {
		t.Fatalf("trimmed repair still overflows: user=%d system=%d", EstimateTokens(user), EstimateTokens(system))
	}
	if !strings.HasPrefix(user, "Previous reply:\n") {
		t.Fatalf("lost header: %q", user[:min(40, len(user))])
	}
}

func TestChatFitsFormer24kChunks(t *testing.T) {
	system := SystemPrompt()
	user := "Chunk 1/8 (cues c0001–c0400):\n" + strings.Repeat("x", 24000)
	if !chatFits(system, user) {
		t.Fatalf("24k cue chunk should fit 32k num_ctx: system=%d user=%d budget=%d", EstimateTokens(system), EstimateTokens(user), contextBudget)
	}
}

func TestCueChunkTargetFitsChatBudget(t *testing.T) {
	system := SystemPrompt()
	target := CueChunkTarget(system, 128)
	if target < 1024 {
		t.Fatalf("target too small: %d", target)
	}
	cues := hourCues(t)
	chunks := ChunkCues(cues, target, target/10)
	if len(chunks) < 2 {
		t.Fatalf("expected split, got %d", len(chunks))
	}
	for i, ch := range chunks {
		user := "Chunk 99/99 (cues c0001–c9999):\n" + ch.Text
		if !chatFits(system, user) {
			t.Fatalf("chunk %d does not fit chat budget: cues=%d bytes=%d target=%d", i, len(ch.Cues), EstimateTokens(user), target)
		}
	}
}

func TestChunkHourPlusSynthetic(t *testing.T) {
	cues := hourCues(t)
	if last := cues[len(cues)-1].End; last < 3600 {
		t.Fatalf("fixture too short: %v", last)
	}
	chunks := ChunkCues(cues, 800, 100)
	if len(chunks) < 2 {
		t.Fatalf("hour-plus should split, got %d chunks", len(chunks))
	}
	// Overlap: next chunk should start before previous end ID.
	for i := 1; i < len(chunks); i++ {
		if chunks[i].StartID > chunks[i-1].EndID {
			t.Fatalf("chunk %d has a cue gap (%d > %d)", i, chunks[i].StartID, chunks[i-1].EndID)
		}
		if chunks[i].StartID < chunks[i-1].StartID {
			t.Fatalf("chunks not advancing")
		}
	}
}

func hourCues(t *testing.T) []Cue {
	t.Helper()
	const n = 4000
	out := make([]Cue, n)
	for i := 0; i < n; i++ {
		start := float64(i) * 0.95
		out[i] = Cue{
			ID:    i + 1,
			Start: start,
			End:   start + 1.1,
			Text:  "synthetic cue text for a long video segment about topic number",
		}
	}
	return out
}
