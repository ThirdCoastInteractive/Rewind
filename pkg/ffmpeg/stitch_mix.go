package ffmpeg

import "fmt"

// StitchLoudnorm is the single-pass fallback when a first-pass measure fails.
var StitchLoudnorm = LoudnormFilter(DefaultLoudnessI, nil)

// MatchLoudnessEnabled reports whether a stitch should loudness-match clips.
// Missing or empty global filters default to ON — quiet sources next to
// loud ones is the usual clipper failure, not an opt-in feature.
func MatchLoudnessEnabled(specs []FilterSpec) bool {
	for _, s := range specs {
		if s.Type != "match_loudness" {
			continue
		}
		if s.Params == nil {
			return true
		}
		switch v := s.Params["enabled"].(type) {
		case bool:
			return v
		case string:
			return v != "false" && v != "0" && v != ""
		case float64:
			return v != 0
		default:
			return true
		}
	}
	return true
}

// StripControlFilters drops stitch UI flags that are not ffmpeg filters.
func StripControlFilters(specs []FilterSpec) []FilterSpec {
	out := make([]FilterSpec, 0, len(specs))
	for _, s := range specs {
		if s.Type == "match_loudness" || s.Type == "burn_captions" {
			continue
		}
		out = append(out, s)
	}
	return out
}

// LookFilterSpecs maps Stitch look chips onto existing filter specs.
func LookFilterSpecs(look string) []FilterSpec {
	switch look {
	case "punch":
		return []FilterSpec{
			{Type: "contrast", Params: map[string]any{"value": 1.12}},
			{Type: "saturation", Params: map[string]any{"value": 1.1}},
		}
	case "warm":
		return []FilterSpec{{Type: "color_temp", Params: map[string]any{"temperature": 4800.0}}}
	case "cool":
		return []FilterSpec{{Type: "color_temp", Params: map[string]any{"temperature": 8200.0}}}
	case "noir":
		return []FilterSpec{
			{Type: "grayscale"},
			{Type: "contrast", Params: map[string]any{"value": 1.2}},
		}
	default:
		return nil
	}
}

// AppendStitchMix adds look, loudness match, and trim gain onto compiled
// per-segment filter lists. Loudnorm runs before user gain so a clipper can
// still nudge a matched clip.
func AppendStitchMix(video, audio []string, look string, gainDb float64, match bool, existing []FilterSpec) (v, a []string) {
	v, a = video, audio
	if specs := LookFilterSpecs(look); len(specs) > 0 {
		if lv, la, err := CompileFilterStrings(specs, nil); err == nil {
			v = append(v, lv...)
			a = append(a, la...)
		}
	}
	hasNorm := false
	for _, f := range existing {
		if f.Type == "normalize" {
			hasNorm = true
			break
		}
	}
	if match && !hasNorm {
		a = append(a, LoudnormFilter(DefaultLoudnessI, nil))
	}
	if gainDb != 0 {
		a = append(a, fmt.Sprintf("volume=%.3fdB", gainDb))
	}
	return v, a
}
