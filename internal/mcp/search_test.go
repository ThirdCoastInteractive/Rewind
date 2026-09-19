package mcp

import (
	"context"
	"strings"
	"testing"

	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/search"
	"thirdcoast.systems/rewind/pkg/captions"
)

func TestCollapsePassagesSameContextWindow(t *testing.T) {
	cw := contextSpan{ID: "cw1", Start: 100, End: 200}
	got := collapsePassages([]passageCandidate{
		{Timestamp: 110, ContextStart: 80, ContextEnd: 140, MatchEvidence: "first", Cues: []captions.Cue{{Start: 108, End: 112, Text: "first"}}, Windows: []contextSpan{cw}},
		{Timestamp: 180, ContextStart: 150, ContextEnd: 210, MatchEvidence: "second", Cues: []captions.Cue{{Start: 178, End: 182, Text: "second"}}, Windows: []contextSpan{cw}},
	})
	if len(got) != 1 {
		t.Fatalf("got %d passages, want 1", len(got))
	}
	if got[0].Timestamp != 110 {
		t.Fatalf("timestamp %v", got[0].Timestamp)
	}
	if got[0].ContextStart != 80 || got[0].ContextEnd != 210 {
		t.Fatalf("union range %v-%v", got[0].ContextStart, got[0].ContextEnd)
	}
	if len(got[0].Cues) != 2 {
		t.Fatalf("cues %d", len(got[0].Cues))
	}
}

func TestCollapsePassagesDifferentContextWindows(t *testing.T) {
	got := collapsePassages([]passageCandidate{
		{Timestamp: 110, ContextStart: 80, ContextEnd: 140, Windows: []contextSpan{{ID: "cw1", Start: 100, End: 150}}},
		{Timestamp: 180, ContextStart: 150, ContextEnd: 210, Windows: []contextSpan{{ID: "cw2", Start: 160, End: 220}}},
	})
	if len(got) != 2 {
		t.Fatalf("got %d passages, want 2", len(got))
	}
}

func TestMatchedCueEvidenceExactAndCrossCue(t *testing.T) {
	cues := []captions.Cue{
		{Start: 1, End: 2, Text: "hello there"},
		{Start: 2, End: 3, Text: "general kenobi"},
		{Start: 8, End: 9, Text: "unrelated"},
	}
	exact := search.Compile(`"hello there"`)
	if got := matchedCueEvidence(cues, 0, exact); got != "hello there" {
		t.Fatalf("exact cue: %q", got)
	}
	if got := matchedCueEvidence(cues, 1, exact); got != "" {
		t.Fatalf("later cue should not claim earlier phrase: %q", got)
	}
	phrase := search.Compile(`"hello there general kenobi"`)
	if got := matchedCueEvidence(cues, 0, phrase); !strings.Contains(got, "hello there") || !strings.Contains(got, "general kenobi") {
		t.Fatalf("cross-cue phrase: %q", got)
	}
	if ts := cues[0].Start; ts != 1 {
		t.Fatalf("timestamp should stay on first contributing cue, got %v", ts)
	}
}

func TestRequireWriteNilToken(t *testing.T) {
	err := requireWrite(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "mcp:write") {
		t.Fatalf("got %v", err)
	}
}

func TestRequireWriteMissingScope(t *testing.T) {
	ctx := withToken(context.Background(), &db.APIToken{Scopes: []string{"mcp:read"}})
	if err := requireWrite(ctx); err == nil {
		t.Fatal("expected error")
	}
}

func TestRequireWriteGranted(t *testing.T) {
	ctx := withToken(context.Background(), &db.APIToken{Scopes: []string{"mcp:read", "mcp:write"}})
	if err := requireWrite(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSourceSelectionMatchesTalkedAbout(t *testing.T) {
	talked := []string{"transcript", "context_windows"}
	if !sourceSelectionMatches(map[string]bool{"transcript": true}, talked) {
		t.Fatal("transcript should match talked-about sources")
	}
	if !sourceSelectionMatches(map[string]bool{"context_windows": true}, talked) {
		t.Fatal("context_windows should match talked-about sources")
	}
	if sourceSelectionMatches(map[string]bool{"title": true, "tags": true}, talked) {
		t.Fatal("title/tags should not match talked-about sources")
	}
	if !sourceSelectionMatches(map[string]bool{"title": true}, nil) {
		t.Fatal("empty sources should match any")
	}
	if !sourceSelectionMatches(map[string]bool{"transcript": true}, []string{"Transcript"}) {
		t.Fatal("sources should be case-insensitive")
	}
}
