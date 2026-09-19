package osint

import (
	"testing"
	"time"
)

func TestRaidBucketMath(t *testing.T) {
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	var stamps []CommentStamp
	// Quiet buckets: 2 comments each in two earlier windows.
	for i := 0; i < 2; i++ {
		stamps = append(stamps, CommentStamp{
			PublishedAt: base.Add(-45 * time.Minute),
			CommenterID: "old-a",
			FirstSeen:   base.Add(-48 * time.Hour),
			CommentID:   "q1",
		})
		stamps = append(stamps, CommentStamp{
			PublishedAt: base.Add(-30 * time.Minute),
			CommenterID: "old-b",
			FirstSeen:   base.Add(-48 * time.Hour),
			CommentID:   "q2",
		})
	}
	// Burst bucket at base: 8 newcomers.
	for i := 0; i < 8; i++ {
		stamps = append(stamps, CommentStamp{
			PublishedAt: base.Add(time.Duration(i) * time.Minute),
			CommenterID: "n" + string(rune('a'+i)),
			FirstSeen:   base.Add(-20 * time.Minute),
			CommentID:   "b" + string(rune('a'+i)),
		})
	}
	buckets := BucketComments(stamps)
	if len(buckets) < 3 {
		t.Fatalf("expected ≥3 buckets, got %d", len(buckets))
	}
	median := MedianNonemptyBucketCount(buckets)
	if median <= 0 {
		t.Fatal("median")
	}
	var found bool
	for _, b := range buckets {
		ok, frac := IsRaidBurst(b, median, 4)
		if ok {
			found = true
			if frac < 0.4 {
				t.Fatalf("newcomer frac %.2f", frac)
			}
		}
	}
	if !found {
		t.Fatalf("expected a raid bucket; median=%.2f buckets=%v", median, summarize(buckets))
	}
}

func summarize(buckets []RaidBucket) []int {
	out := make([]int, len(buckets))
	for i, b := range buckets {
		out[i] = b.Count
	}
	return out
}
