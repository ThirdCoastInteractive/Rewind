package runtimecfg

import (
	"context"
	"math"
	"testing"
)

func TestRegistryDefaultsAndValidation(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range Registry {
		if seen[d.Key] {
			t.Fatalf("duplicate %s", d.Key)
		}
		seen[d.Key] = true
		if err := Validate(d.Key, d.Default); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		key   string
		value any
	}{
		{"unknown", true}, {"agent.max_calls", 0}, {"agent.max_calls", 1.5}, {"agent.max_calls", "64"},
		{"agent.temperature", math.NaN()}, {"agent.temperature", math.Inf(1)},
		{"vision.backfill", "true"}, {"whisper.model", "../../secret"},
		{"agent.model", ""}, {"downloads.user_agent", "a\nb"},
	} {
		if Validate(tc.key, tc.value) == nil {
			t.Errorf("accepted %s=%v", tc.key, tc.value)
		}
	}
}

func TestWhisperModelDefaultTurbo(t *testing.T) {
	if got := Defaults()["whisper.model"]; got != "large-v3-turbo" {
		t.Fatalf("Defaults()[whisper.model]=%v want large-v3-turbo", got)
	}
	if err := Validate("whisper.model", "large-v3-turbo"); err != nil {
		t.Fatalf("Validate large-v3-turbo: %v", err)
	}
	want := map[string]bool{"tiny": true, "base": true, "small": true, "medium": true, "large-v3": true, "large-v3-turbo": true}
	for _, d := range Registry {
		if d.Key != "whisper.model" {
			continue
		}
		for _, c := range d.Choices {
			delete(want, c)
		}
		if len(want) != 0 {
			t.Fatalf("whisper.model choices missing %v (got %v)", want, d.Choices)
		}
		return
	}
	t.Fatal("whisper.model not in Registry")
}

func TestSnapshotHashStable(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b Snapshot
		same bool
	}{
		{
			name: "identical snapshots",
			a:    Snapshot{"agent.max_calls": 64, "vision.backfill": true},
			b:    Snapshot{"agent.max_calls": 64, "vision.backfill": true},
			same: true,
		},
		{
			name: "key insertion order",
			a:    Snapshot{"vision.backfill": true, "agent.max_calls": 64},
			b:    Snapshot{"agent.max_calls": 64, "vision.backfill": true},
			same: true,
		},
		{
			name: "defaults copies",
			a:    Defaults(),
			b:    Defaults(),
			same: true,
		},
		{
			name: "int and float64 encode the same",
			a:    Snapshot{"agent.max_calls": 64},
			b:    Snapshot{"agent.max_calls": float64(64)},
			same: true,
		},
		{
			name: "different values",
			a:    Snapshot{"agent.max_calls": 64},
			b:    Snapshot{"agent.max_calls": 12},
			same: false,
		},
		{
			name: "missing key",
			a:    Snapshot{"agent.max_calls": 64, "vision.backfill": true},
			b:    Snapshot{"agent.max_calls": 64},
			same: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ha, hb := snapshotHash(tc.a), snapshotHash(tc.b)
			if tc.same && ha != hb {
				t.Fatalf("hashes differ: %q vs %q", ha, hb)
			}
			if !tc.same && ha == hb {
				t.Fatalf("hashes unexpectedly equal: %q", ha)
			}
			if tc.same && ha == "" {
				t.Fatal("empty hash")
			}
		})
	}
}

func TestSnapshotDoesNotChangeWithCaller(t *testing.T) {
	s := Defaults()
	s["agent.max_calls"] = 12
	ctx := WithSnapshot(context.Background(), s)
	s["agent.max_calls"] = 99
	if Int(ctx, "agent.max_calls") != 12 {
		t.Fatal("snapshot changed")
	}
	if Env(ctx, "CONTEXT_MODEL") != "qwen3.8:27b" {
		t.Fatal("snapshot environment mapping lost")
	}
}
