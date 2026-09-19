package transcription

import (
	"sort"
	"thirdcoast.systems/rewind/pkg/captions"
)

// ReplaceRange splices source-timed replacement cues into a transcript, retaining
// the portions of boundary-crossing cues outside the repaired interval.
func ReplaceRange(old, replacement []captions.Cue, start, end float64) []captions.Cue {
	out := make([]captions.Cue, 0, len(old)+len(replacement))
	for _, cue := range old {
		if cue.End <= start || cue.Start >= end {
			out = append(out, cue)
			continue
		}
		if cue.Start < start {
			left := cue
			left.End = start
			out = append(out, left)
		}
		if cue.End > end {
			right := cue
			right.Start = end
			out = append(out, right)
		}
	}
	for _, cue := range replacement {
		cue.Start = max(start, cue.Start)
		cue.End = min(end, cue.End)
		if cue.End > cue.Start {
			out = append(out, cue)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}
