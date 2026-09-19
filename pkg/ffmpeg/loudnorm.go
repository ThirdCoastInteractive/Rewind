package ffmpeg

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// DefaultLoudnessI is speech/streaming integrated loudness (LUFS).
// -16 is too quiet for a lot of YouTube dialogue; -14 is the usual target.
const DefaultLoudnessI = -14.0

const loudnessTruePeak = -1.5
const loudnessLRA = 11.0

// LoudnormMeasurement is the first-pass print_format=json result.
type LoudnormMeasurement struct {
	InputI      float64 `json:"input_i"`
	InputTP     float64 `json:"input_tp"`
	InputLRA    float64 `json:"input_lra"`
	InputThresh float64 `json:"input_thresh"`
	TargetOffset float64 `json:"target_offset"`
}

// LoudnormFilter is an EBU R128 loudnorm filter. With measurements it is a
// linear second pass (constant gain to the target). Without, it is a weaker
// single-pass guess.
func LoudnormFilter(targetI float64, m *LoudnormMeasurement) string {
	if targetI == 0 {
		targetI = DefaultLoudnessI
	}
	base := fmt.Sprintf("loudnorm=I=%.1f:TP=%.1f:LRA=%.1f:dual_mono=true", targetI, loudnessTruePeak, loudnessLRA)
	if m == nil || math.IsInf(m.InputI, 0) || math.IsNaN(m.InputI) {
		return base
	}
	return fmt.Sprintf(
		"%s:measured_I=%.2f:measured_TP=%.2f:measured_LRA=%.2f:measured_thresh=%.2f:offset=%.2f:linear=true",
		base, m.InputI, m.InputTP, m.InputLRA, m.InputThresh, m.TargetOffset,
	)
}

// MeasureLoudnorm runs a first pass on a window of the file (the whole range
// if short, otherwise ~90s from the middle) so a long chapter does not get a
// full extra decode.
func MeasureLoudnorm(ctx context.Context, input string, start, dur time.Duration, targetI float64) (*LoudnormMeasurement, error) {
	if input == "" || dur <= 0 {
		return nil, fmt.Errorf("loudnorm measure: empty input")
	}
	if targetI == 0 {
		targetI = DefaultLoudnessI
	}
	ss, sample := loudnormSampleWindow(start, dur)
	args := []string{
		"-hide_banner", "-nostats", "-y",
		"-ss", formatDuration(ss),
		"-t", formatDuration(sample),
		"-i", input,
		"-vn", "-sn",
		"-af", LoudnormFilter(targetI, nil)+":print_format=json",
		"-f", "null",
		"-",
	}
	res := runCapture(ctx, args)
	m, err := ParseLoudnormLogs(res.Logs)
	if err != nil {
		if res.Err != nil {
			return nil, fmt.Errorf("loudnorm measure: %w", res.Err)
		}
		return nil, err
	}
	return m, nil
}

func loudnormSampleWindow(start, dur time.Duration) (ss, sample time.Duration) {
	const maxSample = 90 * time.Second
	if dur <= maxSample {
		return start, dur
	}
	ss = start + dur/2 - maxSample/2
	if ss < start {
		ss = start
	}
	return ss, maxSample
}

// ParseLoudnormLogs extracts the JSON object ffmpeg loudnorm prints on stderr.
func ParseLoudnormLogs(logs string) (*LoudnormMeasurement, error) {
	start := strings.LastIndex(logs, "{")
	end := strings.LastIndex(logs, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("loudnorm measure: no json in ffmpeg log")
	}
	raw := logs[start : end+1]
	var wire struct {
		InputI       json.Number `json:"input_i"`
		InputTP      json.Number `json:"input_tp"`
		InputLRA     json.Number `json:"input_lra"`
		InputThresh  json.Number `json:"input_thresh"`
		TargetOffset json.Number `json:"target_offset"`
	}
	if err := json.Unmarshal([]byte(raw), &wire); err != nil {
		return nil, fmt.Errorf("loudnorm measure: %w", err)
	}
	m := &LoudnormMeasurement{
		InputI:       parseLoudnormNumber(wire.InputI),
		InputTP:      parseLoudnormNumber(wire.InputTP),
		InputLRA:     parseLoudnormNumber(wire.InputLRA),
		InputThresh:  parseLoudnormNumber(wire.InputThresh),
		TargetOffset: parseLoudnormNumber(wire.TargetOffset),
	}
	if math.IsInf(m.InputI, 0) || math.IsNaN(m.InputI) {
		return nil, fmt.Errorf("loudnorm measure: silent or unreadable audio")
	}
	return m, nil
}

func parseLoudnormNumber(n json.Number) float64 {
	s := strings.TrimSpace(n.String())
	if s == "" || strings.EqualFold(s, "-inf") || strings.EqualFold(s, "inf") {
		return math.Inf(-1)
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return math.NaN()
	}
	return v
}

// ReplaceLoudnorm swaps any loudnorm filter for a measured second-pass.
func ReplaceLoudnorm(filters []string, targetI float64, m *LoudnormMeasurement) []string {
	next := LoudnormFilter(targetI, m)
	out := make([]string, 0, len(filters)+1)
	found := false
	for _, f := range filters {
		if strings.HasPrefix(f, "loudnorm=") || f == "loudnorm" {
			out = append(out, next)
			found = true
			continue
		}
		out = append(out, f)
	}
	if !found {
		out = append(out, next)
	}
	return out
}

// MatchLoudnessTarget reads match_loudness.params.target (LUFS). Default -14.
func MatchLoudnessTarget(specs []FilterSpec) float64 {
	for _, s := range specs {
		if s.Type != "match_loudness" || s.Params == nil {
			continue
		}
		switch v := s.Params["target"].(type) {
		case float64:
			return clampLoudnessI(v)
		case json.Number:
			f, err := v.Float64()
			if err == nil {
				return clampLoudnessI(f)
			}
		case string:
			f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err == nil {
				return clampLoudnessI(f)
			}
		}
	}
	return DefaultLoudnessI
}

func clampLoudnessI(v float64) float64 {
	if v > -10 {
		return -10
	}
	if v < -24 {
		return -24
	}
	return v
}
