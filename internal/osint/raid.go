package osint

import (
	"sort"
	"time"
)

const raidBucketSeconds = 15 * 60

// CommentStamp is a comment timestamp plus its author for raid math.
type CommentStamp struct {
	PublishedAt time.Time
	CommenterID string
	FirstSeen   time.Time
	CommentID   string
}

// RaidBucket is one 15-minute window of comments on a video.
type RaidBucket struct {
	Start    time.Time
	Count    int
	Comments []CommentStamp
}

// BucketComments groups stamps into fixed 15-minute UTC buckets.
func BucketComments(stamps []CommentStamp) []RaidBucket {
	if len(stamps) == 0 {
		return nil
	}
	groups := map[int64][]CommentStamp{}
	for _, s := range stamps {
		if s.PublishedAt.IsZero() {
			continue
		}
		unix := s.PublishedAt.UTC().Unix()
		key := unix - (unix % raidBucketSeconds)
		groups[key] = append(groups[key], s)
	}
	keys := make([]int64, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	out := make([]RaidBucket, 0, len(keys))
	for _, k := range keys {
		out = append(out, RaidBucket{
			Start:    time.Unix(k, 0).UTC(),
			Count:    len(groups[k]),
			Comments: groups[k],
		})
	}
	return out
}

// MedianNonemptyBucketCount returns the median count among buckets with ≥1 comment.
func MedianNonemptyBucketCount(buckets []RaidBucket) float64 {
	var counts []int
	for _, b := range buckets {
		if b.Count > 0 {
			counts = append(counts, b.Count)
		}
	}
	if len(counts) == 0 {
		return 0
	}
	sort.Ints(counts)
	mid := len(counts) / 2
	if len(counts)%2 == 0 {
		return float64(counts[mid-1]+counts[mid]) / 2
	}
	return float64(counts[mid])
}

// IsRaidBurst reports whether a bucket looks like a coordinated raid.
// ratio is typically osint.raid_ratio (default 4).
func IsRaidBurst(bucket RaidBucket, median float64, ratio float64) (ok bool, newcomerFrac float64) {
	if bucket.Count < 8 || median <= 0 || ratio < 1 {
		return false, 0
	}
	if float64(bucket.Count) < ratio*median {
		return false, 0
	}
	authors := map[string]CommentStamp{}
	for _, c := range bucket.Comments {
		if c.CommenterID == "" {
			continue
		}
		if _, seen := authors[c.CommenterID]; !seen {
			authors[c.CommenterID] = c
		}
	}
	if len(authors) == 0 {
		return false, 0
	}
	windowStart := bucket.Start.Add(-time.Hour)
	windowEnd := bucket.Start.Add(15*time.Minute + time.Hour)
	newcomers := 0
	for _, c := range authors {
		if !c.FirstSeen.IsZero() && !c.FirstSeen.Before(windowStart) && !c.FirstSeen.After(windowEnd) {
			newcomers++
		}
	}
	frac := float64(newcomers) / float64(len(authors))
	return frac >= 0.40, frac
}
