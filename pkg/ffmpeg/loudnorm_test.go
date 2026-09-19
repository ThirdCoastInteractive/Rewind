package ffmpeg

import (
	"strings"
	"testing"
	"time"
)

func TestParseLoudnormLogs(t *testing.T) {
	t.Parallel()
	logs := `
[Parsed_loudnorm_0 @ 0x0]
{
	"input_i" : "-24.53",
	"input_tp" : "-4.98",
	"input_lra" : "8.30",
	"input_thresh" : "-35.12",
	"output_i" : "-14.02",
	"output_tp" : "-1.50",
	"output_lra" : "8.30",
	"output_thresh" : "-24.78",
	"normalization_type" : "dynamic",
	"target_offset" : "0.02"
}
`
	m, err := ParseLoudnormLogs(logs)
	if err != nil {
		t.Fatal(err)
	}
	if m.InputI > -24.5 || m.InputI < -24.6 {
		t.Fatalf("input_i=%v", m.InputI)
	}
	if m.TargetOffset < 0.01 || m.TargetOffset > 0.03 {
		t.Fatalf("offset=%v", m.TargetOffset)
	}
}

func TestLoudnormFilterMeasured(t *testing.T) {
	t.Parallel()
	m := &LoudnormMeasurement{InputI: -24.5, InputTP: -5, InputLRA: 8, InputThresh: -35, TargetOffset: 0.1}
	got := LoudnormFilter(-14, m)
	if !strings.Contains(got, "measured_I=-24.50") {
		t.Fatalf("%s", got)
	}
	if !strings.Contains(got, "linear=true") {
		t.Fatalf("%s", got)
	}
	if !strings.Contains(got, "I=-14.0") {
		t.Fatalf("%s", got)
	}
}

func TestReplaceLoudnorm(t *testing.T) {
	t.Parallel()
	in := []string{"aresample=48000", "loudnorm=I=-16:TP=-1.5:LRA=11", "volume=2dB"}
	m := &LoudnormMeasurement{InputI: -28, InputTP: -8, InputLRA: 6, InputThresh: -40, TargetOffset: 0}
	out := ReplaceLoudnorm(in, -12, m)
	if len(out) != 3 {
		t.Fatalf("%v", out)
	}
	if !strings.Contains(out[1], "measured_I=-28.00") || !strings.Contains(out[1], "I=-12.0") {
		t.Fatalf("%v", out)
	}
}

func TestMatchLoudnessTarget(t *testing.T) {
	t.Parallel()
	if got := MatchLoudnessTarget(nil); got != DefaultLoudnessI {
		t.Fatalf("default %v", got)
	}
	if got := MatchLoudnessTarget([]FilterSpec{{Type: "match_loudness", Params: map[string]any{"enabled": true, "target": -12.0}}}); got != -12 {
		t.Fatalf("got %v", got)
	}
}

func TestLoudnormSampleWindow(t *testing.T) {
	t.Parallel()
	ss, sample := loudnormSampleWindow(0, 30*time.Second)
	if ss != 0 || sample != 30*time.Second {
		t.Fatalf("%v %v", ss, sample)
	}
	ss, sample = loudnormSampleWindow(100*time.Second, 40*time.Minute)
	if sample != 90*time.Second {
		t.Fatalf("sample %v", sample)
	}
	if ss < 100*time.Second {
		t.Fatalf("ss %v", ss)
	}
}
