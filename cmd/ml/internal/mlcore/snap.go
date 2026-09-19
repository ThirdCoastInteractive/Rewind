package mlcore

import "math"

// SnapToCues moves start/end to the nearest cue bounds when override_bounds is
// false. Used by refine_boundaries.
func SnapToCues(w Window, cues []Cue) Window {
	if w.OverrideBounds || len(cues) == 0 {
		return w
	}
	w.Start = nearestCueStart(cues, w.Start)
	w.End = nearestCueEnd(cues, w.End)
	if w.End <= w.Start {
		w.End = w.Start + 0.01
	}
	return w
}

func nearestCueStart(cues []Cue, t float64) float64 {
	best := cues[0].Start
	bestD := math.Abs(cues[0].Start - t)
	for _, c := range cues[1:] {
		d := math.Abs(c.Start - t)
		if d < bestD {
			bestD = d
			best = c.Start
		}
	}
	return best
}

func nearestCueEnd(cues []Cue, t float64) float64 {
	best := cues[0].End
	bestD := math.Abs(cues[0].End - t)
	for _, c := range cues[1:] {
		d := math.Abs(c.End - t)
		if d < bestD {
			bestD = d
			best = c.End
		}
	}
	return best
}
