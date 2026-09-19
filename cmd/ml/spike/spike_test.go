package spike_test

import (
	"testing"

	"thirdcoast.systems/rewind/cmd/ml/internal/mlcore"
	"thirdcoast.systems/rewind/pkg/captions"
)

// Spike fixtures live in cmd/ml/internal/mlcore tests. This file keeps
// `go test ./cmd/ml/spike` covering chunk + reconcile without Ollama or GPU.

func TestSpikeShortCuesReconcile(t *testing.T) {
	cues := mlcore.CuesFromCaptions([]captions.Cue{
		{Start: 0, End: 1, Text: "one"},
		{Start: 1, End: 2, Text: "two"},
		{Start: 2, End: 3, Text: "three"},
	})
	chunks := mlcore.ChunkCues(cues, mlcore.DefaultTargetTokens, mlcore.DefaultOverlapTokens)
	if len(chunks) != 1 {
		t.Fatalf("chunks=%d", len(chunks))
	}
	wins := mlcore.WindowsFromModel([]mlcore.ModelWindow{
		{Title: "All", Summary: "short", CueStart: 1, CueEnd: 3},
	}, cues)
	out := mlcore.Reconcile(wins, 3)
	if err := mlcore.WindowsCovered(out, 3); err != nil {
		t.Fatal(err)
	}
}
