package main

import "testing"

func TestClaimSlotLimitCUDATranscribeIsSerial(t *testing.T) {
	got := claimSlotLimit([]string{"transcribe"}, 2, nil, "cuda")
	if got != 1 {
		t.Fatalf("cuda transcribe slots = %d, want 1", got)
	}
	got = claimSlotLimit([]string{"transcribe"}, 2, nil, "cpu")
	if got != 2 {
		t.Fatalf("cpu transcribe slots = %d, want 2", got)
	}
}

func TestClaimSlotLimitMixedGPUKindsSerial(t *testing.T) {
	mixed := []string{"visual_index", "transcribe", "context_windows", "refine_boundaries"}
	if got := claimSlotLimit(mixed, 4, nil, "cuda"); got != 1 {
		t.Fatalf("cuda mixed slots = %d, want 1", got)
	}
	if got := claimSlotLimit(mixed, 4, nil, "rocm"); got != 1 {
		t.Fatalf("rocm mixed slots = %d, want 1", got)
	}
}
