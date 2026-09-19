// Package waveform finds edit-safe boundaries in Rewind waveform assets.
package waveform

import (
	"encoding/binary"
	"math"
	"sort"
)

const (
	// DefaultBucketSeconds is the waveform asset interval.
	DefaultBucketSeconds = 0.1
	// DefaultRadiusSeconds is the search radius around a proposed edge.
	DefaultRadiusSeconds = 5.0
	// MinValleySeconds rejects momentary low samples inside speech.
	MinValleySeconds = 0.3
	// QuietPercentile is the local-window percentile used as the quiet floor.
	QuietPercentile = 0.20
	// QuietRangeShare raises the valley threshold a fraction of the way toward the peak.
	QuietRangeShare = 0.08
)

// Candidate is a ranked quiet edit boundary.
type Candidate struct {
	TimeSeconds    float64 `json:"time_seconds"`
	Score          float64 `json:"score"`
	MeanAmplitude  float64 `json:"mean_amplitude"`
	ValleyDuration float64 `json:"valley_duration"`
}

// DecodeI16LE decodes unsigned absolute int16 magnitudes stored little-endian.
func DecodeI16LE(data []byte) []int16 {
	out := make([]int16, len(data)/2)
	for i := range out {
		out[i] = int16(binary.LittleEndian.Uint16(data[i*2:]))
		if out[i] < 0 {
			out[i] = -out[i]
		}
	}
	return out
}

// Suggest returns up to limit quiet valleys around proposed, frame-snapped.
func Suggest(peaks []int16, bucketSeconds, proposed, radius, fps float64, limit int) []Candidate {
	if len(peaks) == 0 || bucketSeconds <= 0 || limit <= 0 {
		return nil
	}
	if radius <= 0 {
		radius = DefaultRadiusSeconds
	}
	lo := max(0, int(math.Floor((proposed-radius)/bucketSeconds)))
	hi := min(len(peaks)-1, int(math.Ceil((proposed+radius)/bucketSeconds)))
	if hi < lo {
		return nil
	}
	local := make([]float64, 0, hi-lo+1)
	for i := lo; i <= hi; i++ {
		local = append(local, math.Abs(float64(peaks[i])))
	}
	sorted := append([]float64(nil), local...)
	sort.Float64s(sorted)
	p20 := sorted[int(math.Floor(float64(len(sorted)-1)*QuietPercentile))]
	threshold := p20 + (sorted[len(sorted)-1]-p20)*QuietRangeShare
	minBuckets := max(1, int(math.Ceil(MinValleySeconds/bucketSeconds)))

	var candidates []Candidate
	for i := lo; i <= hi; {
		if math.Abs(float64(peaks[i])) > threshold {
			i++
			continue
		}
		start := i
		sum := 0.0
		for i <= hi && math.Abs(float64(peaks[i])) <= threshold {
			sum += math.Abs(float64(peaks[i]))
			i++
		}
		count := i - start
		if count < minBuckets {
			continue
		}
		center := (float64(start+i-1) / 2) * bucketSeconds
		if fps > 0 {
			center = math.Round(center*fps) / fps
		}
		mean := sum / float64(count)
		duration := float64(count) * bucketSeconds
		normAmp := mean / math.Max(1, threshold)
		distance := math.Abs(center-proposed) / radius
		score := (1-normAmp)*0.55 + math.Min(duration/2, 1)*0.25 + (1-math.Min(distance, 1))*0.20
		candidates = append(candidates, Candidate{TimeSeconds: center, Score: score, MeanAmplitude: mean, ValleyDuration: duration})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			return math.Abs(candidates[i].TimeSeconds-proposed) < math.Abs(candidates[j].TimeSeconds-proposed)
		}
		return candidates[i].Score > candidates[j].Score
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates
}
