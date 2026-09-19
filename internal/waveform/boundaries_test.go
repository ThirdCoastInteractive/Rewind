package waveform

import "testing"

// TestSuggestFixtures locks the quiet-valley scorer shared with
// static/js/lib/waveform-renderer.js. JS duplicates DefaultBucketSeconds,
// DefaultRadiusSeconds, MinValleySeconds, QuietPercentile, QuietRangeShare,
// and the score weights (0.55 / 0.25 / 0.20) so the two stay in lockstep.
func TestSuggestFixtures(t *testing.T) {
	const (
		bucket   = DefaultBucketSeconds
		proposed = 4.0
		radius   = DefaultRadiusSeconds
		fps      = 30.0
		limit    = 3
	)
	suggest := func(peaks []int16) []Candidate {
		return Suggest(peaks, bucket, proposed, radius, fps, limit)
	}

	t.Run("nil and empty peaks", func(t *testing.T) {
		if got := suggest(nil); got != nil {
			t.Errorf("nil peaks: got %v, want nil", got)
		}
		if got := suggest([]int16{}); got != nil {
			t.Errorf("empty peaks: got %v, want nil", got)
		}
	})

	t.Run("silence", func(t *testing.T) {
		// 100 zeros: every sample is ≤ threshold, so the local window is one valley.
		got := suggest(filled(100, 0))
		if len(got) != 1 {
			t.Fatalf("got %d candidates, want 1", len(got))
		}
		if got[0].TimeSeconds != 4.5 {
			t.Errorf("snapped center = %v, want 4.5", got[0].TimeSeconds)
		}
	})

	t.Run("constant noise", func(t *testing.T) {
		// p20=12000, threshold=12000; > threshold is false, so the whole window is one valley.
		got := suggest(filled(100, 12000))
		if len(got) != 1 {
			t.Fatalf("got %d candidates, want 1", len(got))
		}
		if got[0].TimeSeconds != 4.5 {
			t.Errorf("snapped center = %v, want 4.5", got[0].TimeSeconds)
		}
	})

	t.Run("speech gap", func(t *testing.T) {
		// Proposed 4.0s is at the gap (index 40). Whole-window / gap centers coincide near 4.35s.
		peaks := append(append(filled(40, 20000), filled(8, 50)...), filled(40, 20000)...)
		got := suggest(peaks)
		if len(got) == 0 {
			t.Fatal("got no candidates")
		}
		t0 := got[0].TimeSeconds
		if t0 < 4.35 || t0 > 4.4 {
			t.Errorf("best candidate time = %v, want in [4.35, 4.4] (gap center)", t0)
		}
	})

	t.Run("frame snap", func(t *testing.T) {
		// 88 samples: unsnapped center is 4.35s, off both the 30fps and 1fps grids.
		peaks := filled(88, 0)
		got30 := Suggest(peaks, bucket, proposed, radius, 30, limit)
		got1 := Suggest(peaks, bucket, proposed, radius, 1, limit)
		if len(got30) == 0 || len(got1) == 0 {
			t.Fatalf("got fps30=%d fps1=%d candidates, want both non-empty", len(got30), len(got1))
		}
		if got30[0].TimeSeconds == got1[0].TimeSeconds {
			t.Errorf("fps 30 and fps 1 both snapped to %v; want different times for off-frame center", got30[0].TimeSeconds)
		}
	})
}

func filled(n int, v int16) []int16 {
	out := make([]int16, n)
	for i := range out {
		out[i] = v
	}
	return out
}
