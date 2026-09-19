package encode

import (
	"strings"
	"testing"
)

func TestStitchWorkerIDDistinctPerSlot(t *testing.T) {
	base := "encoder-host"
	a := stitchWorkerID(base, 0)
	b := stitchWorkerID(base, 1)
	if a == b {
		t.Fatalf("worker IDs collided: %q", a)
	}
	if !strings.HasPrefix(a, base+"-stitch-") || !strings.HasSuffix(a, "-0") {
		t.Fatalf("unexpected id %q", a)
	}
	if !strings.HasSuffix(b, "-1") {
		t.Fatalf("unexpected id %q", b)
	}
}

func TestStitchWorkerSlotsMatchClipExportPattern(t *testing.T) {
	// Clip exports launch 32 goroutines gated by processing.encoder_workers.
	// Stitch must do the same so short exports are not blocked behind one long dump.
	const slots = 32
	seen := make(map[string]bool, slots)
	for i := 0; i < slots; i++ {
		id := stitchWorkerID("encoder-test", i)
		if seen[id] {
			t.Fatalf("duplicate worker id %q", id)
		}
		seen[id] = true
	}
	if len(seen) != slots {
		t.Fatalf("got %d unique ids, want %d", len(seen), slots)
	}
}
