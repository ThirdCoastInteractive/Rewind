package mlcore

// ApplyOverrides copies human override_* fields onto new windows by time overlap
// when transcript_hash is unchanged. If the hash changed, new windows are left
// as generated (caller marks prior override rows stale; they are not deleted).
func ApplyOverrides(generated []Window, previous []Window, sameTranscriptHash bool) []Window {
	if len(previous) == 0 || !sameTranscriptHash {
		return generated
	}
	out := make([]Window, len(generated))
	copy(out, generated)
	for i := range out {
		prev, ok := bestOverlap(out[i], previous)
		if !ok {
			continue
		}
		if prev.OverrideTitle {
			out[i].Title = prev.Title
			out[i].OverrideTitle = true
		}
		if prev.OverrideSummary {
			out[i].Summary = prev.Summary
			out[i].Topics = append([]string(nil), prev.Topics...)
			out[i].Entities = append([]string(nil), prev.Entities...)
			out[i].OverrideSummary = true
		}
		if prev.OverrideBounds {
			out[i].Start = prev.Start
			out[i].End = prev.End
			out[i].OverrideBounds = true
		}
	}
	return out
}

func bestOverlap(w Window, prev []Window) (Window, bool) {
	var best Window
	var bestSec float64
	found := false
	for _, p := range prev {
		if p.Stale {
			continue
		}
		if !p.OverrideTitle && !p.OverrideSummary && !p.OverrideBounds {
			continue
		}
		sec := overlapSeconds(w, p)
		if sec > bestSec {
			bestSec = sec
			best = p
			found = true
		}
	}
	return best, found && bestSec > 0
}
