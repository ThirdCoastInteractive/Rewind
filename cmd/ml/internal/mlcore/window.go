package mlcore

import (
	"math"
	"sort"
	"strings"
)

// Window is a generated (or override-bearing) context window in seconds.
type Window struct {
	Start            float64
	End              float64
	Title            string
	Summary          string
	Topics           []string
	Entities         []string
	CueStart         int
	CueEnd           int
	Confidence       float64
	GeneratedStart   float64
	GeneratedEnd     float64
	OverrideTitle    bool
	OverrideSummary  bool
	OverrideBounds   bool
	Stale            bool
	HasGeneratedSpan bool
	Kind             string   `json:"kind,omitempty"`
	Hook             string   `json:"hook,omitempty"`
	Shorts           []Window `json:"shorts,omitempty"`
}

func (w Window) Duration() float64 {
	if w.End <= w.Start {
		return 0
	}
	return w.End - w.Start
}

func overlapSeconds(a, b Window) float64 {
	lo := math.Max(a.Start, b.Start)
	hi := math.Min(a.End, b.End)
	if hi <= lo {
		return 0
	}
	return hi - lo
}

// Reconcile produces contiguous, non-overlapping, full-coverage windows over
// [0, duration]. There is no max duration: an hour-plus single window is kept.
func Reconcile(in []Window, duration float64) []Window {
	if duration < 0 {
		duration = 0
	}
	var maxEnd float64
	cleaned := make([]Window, 0, len(in))
	for _, w := range in {
		if w.End <= w.Start {
			continue
		}
		if w.Start < 0 {
			w.Start = 0
		}
		if duration > 0 && w.End > duration {
			w.End = duration
		}
		if w.End <= w.Start {
			continue
		}
		if !w.HasGeneratedSpan {
			w.GeneratedStart = w.Start
			w.GeneratedEnd = w.End
			w.HasGeneratedSpan = true
		}
		if w.End > maxEnd {
			maxEnd = w.End
		}
		cleaned = append(cleaned, w)
	}
	if duration <= 0 {
		duration = maxEnd
	}
	if duration <= 0 {
		return nil
	}
	if len(cleaned) == 0 {
		return []Window{{
			Start: 0, End: duration,
			Title: "Full video", Summary: "",
			GeneratedStart: 0, GeneratedEnd: duration, HasGeneratedSpan: true,
		}}
	}

	sort.SliceStable(cleaned, func(i, j int) bool {
		if cleaned[i].Start == cleaned[j].Start {
			return cleaned[i].End < cleaned[j].End
		}
		return cleaned[i].Start < cleaned[j].Start
	})

	out := make([]Window, 0, len(cleaned))
	for _, w := range cleaned {
		if len(out) == 0 {
			out = append(out, w)
			continue
		}
		prev := &out[len(out)-1]
		if w.Start < prev.End {
			// Overlap: snap previous end to this start. If that empties prev, merge.
			if w.Start > prev.Start {
				prev.End = w.Start
			} else {
				// w nested at the same start: keep the longer span's metadata on prev.
				if w.End > prev.End {
					prev.End = w.End
				}
				prev.Title = firstNonEmpty(prev.Title, w.Title)
				prev.Summary = firstNonEmpty(prev.Summary, w.Summary)
				continue
			}
			if prev.End <= prev.Start {
				// absorb prev into w
				w.Start = prev.Start
				out[len(out)-1] = w
				continue
			}
		}
		out = append(out, w)
	}

	if len(out) == 0 {
		return []Window{{Start: 0, End: duration, Title: "Full video", GeneratedStart: 0, GeneratedEnd: duration, HasGeneratedSpan: true}}
	}

	// Full coverage: extend first back to 0, last to duration, fill gaps by
	// stretching the earlier window forward (no inserted fillers).
	out[0].Start = 0
	for i := 0; i < len(out)-1; i++ {
		if out[i].End < out[i+1].Start {
			out[i].End = out[i+1].Start
		}
		if out[i].End > out[i+1].Start {
			out[i].End = out[i+1].Start
		}
	}
	if out[len(out)-1].End < duration {
		out[len(out)-1].End = duration
	}
	if duration > 0 && out[len(out)-1].End > duration {
		out[len(out)-1].End = duration
	}

	// Drop any window emptied by snapping.
	final := make([]Window, 0, len(out))
	for _, w := range out {
		if w.End > w.Start {
			final = append(final, w)
		}
	}
	if len(final) == 0 {
		return []Window{{Start: 0, End: duration, Title: "Full video", GeneratedStart: 0, GeneratedEnd: duration, HasGeneratedSpan: true}}
	}
	return final
}

const (
	MinShortSeconds = 8
	MaxShortSeconds = 45
)

// FlattenGenerated splits chapter windows from nested/loose shorts before reconcile.
func FlattenGenerated(in []Window) (parents, shorts []Window) {
	for _, w := range in {
		shorts = append(shorts, w.Shorts...)
		w.Shorts = nil
		if w.Kind == "short" {
			shorts = append(shorts, w)
			continue
		}
		w.Kind = "window"
		parents = append(parents, w)
	}
	return parents, shorts
}

// AttachShorts nests 8-45s shorts under the chapter that contains their midpoint.
func AttachShorts(parents, shorts []Window) []Window {
	if len(parents) == 0 {
		return parents
	}
	out := make([]Window, len(parents))
	copy(out, parents)
	for i := range out {
		out[i].Shorts = nil
	}
	sort.SliceStable(shorts, func(i, j int) bool {
		if shorts[i].Start == shorts[j].Start {
			return shorts[i].End < shorts[j].End
		}
		return shorts[i].Start < shorts[j].Start
	})
	for _, s := range shorts {
		if s.Duration() < MinShortSeconds || s.Duration() > MaxShortSeconds {
			continue
		}
		mid := (s.Start + s.End) / 2
		pi := -1
		for i, p := range out {
			if mid >= p.Start && (mid < p.End || (i == len(out)-1 && mid <= p.End)) {
				pi = i
				break
			}
		}
		if pi < 0 {
			continue
		}
		if s.Start < out[pi].Start {
			s.Start = out[pi].Start
		}
		if s.End > out[pi].End {
			s.End = out[pi].End
		}
		if s.Duration() < MinShortSeconds {
			continue
		}
		overlap := false
		for _, e := range out[pi].Shorts {
			o := overlapSeconds(e, s)
			if o > 0.5*s.Duration() || o > 0.5*e.Duration() {
				overlap = true
				break
			}
		}
		if overlap {
			continue
		}
		s.Kind = "short"
		if strings.TrimSpace(s.Title) == "" {
			s.Title = "Short"
		}
		out[pi].Shorts = append(out[pi].Shorts, s)
	}
	return out
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// WindowsCovered reports whether windows are gap-free, overlap-free, and cover [0, duration].
func WindowsCovered(windows []Window, duration float64) error {
	if duration <= 0 {
		return nil
	}
	if len(windows) == 0 {
		return errf("no windows")
	}
	if windows[0].Start > 1e-6 {
		return errf("gap at start: first begins at %v", windows[0].Start)
	}
	for i, w := range windows {
		if w.End <= w.Start {
			return errf("window %d empty", i)
		}
		if i > 0 {
			prev := windows[i-1]
			if w.Start < prev.End-1e-6 {
				return errf("overlap at %d: prev.end=%v start=%v", i, prev.End, w.Start)
			}
			if w.Start > prev.End+1e-6 {
				return errf("gap at %d: prev.end=%v start=%v", i, prev.End, w.Start)
			}
		}
	}
	last := windows[len(windows)-1]
	if last.End < duration-1e-6 {
		return errf("gap at end: last.end=%v duration=%v", last.End, duration)
	}
	return nil
}

type coverError string

func (e coverError) Error() string { return string(e) }

func errf(format string, args ...any) error {
	return coverError(sprintf(format, args...))
}
