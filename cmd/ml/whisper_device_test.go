package main

import (
	"slices"
	"testing"
)

func TestWhisperCPUSelectionDisablesGPU(t *testing.T) {
	for _, device := range []string{"cpu", "auto", "cuda"} {
		args := whisperCLIArgs("weights.bin", "audio.wav", "out", whisperConfig{Device: device})
		if slices.Contains(args, "-ng") != (device == "cpu") {
			t.Fatalf("device %s: %v", device, args)
		}
	}
}
