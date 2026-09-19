package vision

import "testing"

func TestSampleTimesStayInsideRange(t *testing.T) {
	t.Parallel()
	got := sampleTimes(0, 25, 5, 0, 32)
	want := []float64{0, 5, 10, 15, 20}
	if len(got) != len(want) {
		t.Fatalf("%v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%v", got)
		}
	}
	dense := sampleTimes(0, 25, 1, 0, 32)
	if len(dense) != 25 || dense[0] != 0 || dense[24] != 24 {
		t.Fatalf("%v", dense)
	}
	if leftover := sampleTimes(0, 25, 5, 5, 32); leftover != nil {
		t.Fatalf("expected no leftover samples: %v", leftover)
	}
}
