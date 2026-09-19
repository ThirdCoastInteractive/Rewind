package ffmpeg

import (
	"strings"
	"testing"
)

func TestBurnCaptionsEnabledDefaultOff(t *testing.T) {
	t.Parallel()
	if BurnCaptionsEnabled(nil) {
		t.Fatal("empty globals should not burn captions")
	}
	if BurnCaptionsEnabled([]FilterSpec{{Type: "burn_captions", Params: map[string]any{"enabled": false}}}) {
		t.Fatal("explicit off")
	}
	if !BurnCaptionsEnabled([]FilterSpec{{Type: "burn_captions", Params: map[string]any{"enabled": true}}}) {
		t.Fatal("explicit on")
	}
}

func TestStripControlFiltersDropsBurnCaptions(t *testing.T) {
	t.Parallel()
	got := StripControlFilters([]FilterSpec{
		{Type: "match_loudness", Params: map[string]any{"enabled": true}},
		{Type: "burn_captions", Params: map[string]any{"enabled": true}},
		{Type: "contrast", Params: map[string]any{"value": 1.1}},
	})
	if len(got) != 1 || got[0].Type != "contrast" {
		t.Fatalf("got %#v", got)
	}
}

func TestMatchLoudnessEnabledDefaultOn(t *testing.T) {
	t.Parallel()
	if !MatchLoudnessEnabled(nil) {
		t.Fatal("empty globals should match")
	}
	if MatchLoudnessEnabled([]FilterSpec{{Type: "match_loudness", Params: map[string]any{"enabled": false}}}) {
		t.Fatal("explicit off")
	}
	if !MatchLoudnessEnabled([]FilterSpec{{Type: "match_loudness", Params: map[string]any{"enabled": true}}}) {
		t.Fatal("explicit on")
	}
}

func TestAppendStitchMixLoudnormAndGain(t *testing.T) {
	t.Parallel()
	v, a := AppendStitchMix(nil, nil, "punch", 3, true, nil)
	if len(v) == 0 {
		t.Fatal("expected look video filters")
	}
	foundNorm, foundGain := false, false
	for _, s := range a {
		if strings.HasPrefix(s, "loudnorm=") {
			foundNorm = true
		}
		if s == "volume=3.000dB" {
			foundGain = true
		}
	}
	if !foundNorm || !foundGain {
		t.Fatalf("audio=%v", a)
	}
}

func TestAppendStitchMixSkipsWhenNormalizePresent(t *testing.T) {
	t.Parallel()
	_, a := AppendStitchMix(nil, nil, "", 0, true, []FilterSpec{{Type: "normalize"}})
	for _, s := range a {
		if strings.HasPrefix(s, "loudnorm=") {
			t.Fatal("should not double-normalize")
		}
	}
}
