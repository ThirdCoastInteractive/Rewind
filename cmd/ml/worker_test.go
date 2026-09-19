package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"thirdcoast.systems/rewind/cmd/ml/internal/mlcore"
	"thirdcoast.systems/rewind/internal/vision"
)

func TestMLJobOutcomeInfrastructure(t *testing.T) {
	err := fmt.Errorf("%w: whisper.cpp needs a CUDA runtime", mlcore.ErrInfrastructure)
	status, lastErr, retry := mlJobOutcome(err, 0)
	if status != "waiting_model" {
		t.Fatalf("status=%s want waiting_model", status)
	}
	if retry != 0 {
		t.Fatalf("retrySeconds=%d want 0 (kind cooldown, not retry_wait)", retry)
	}
	if lastErr == "" {
		t.Fatal("expected last error")
	}
}

func TestMLJobOutcomeWaitingModel(t *testing.T) {
	err := fmt.Errorf("%w: whisper model not found", mlcore.ErrWaitingModel)
	status, _, retry := mlJobOutcome(err, 0)
	if status != "waiting_model" || retry != 120 {
		t.Fatalf("status=%s retry=%d", status, retry)
	}
}

func TestMLJobOutcomeOrdinaryRetry(t *testing.T) {
	status, _, retry := mlJobOutcome(errors.New("whisper.cpp failed: boom"), 0)
	if status != "retry_wait" || retry != 60 {
		t.Fatalf("status=%s retry=%d", status, retry)
	}
}

func TestClaimSlotLimitContextSharesModel(t *testing.T) {
	if got := claimSlotLimit([]string{"context_windows", "refine_boundaries"}, 8, nil, ""); got != 1 {
		t.Fatalf("cold context slots=%d want 1 (one loader)", got)
	}
	if got := claimSlotLimit([]string{"context_windows"}, 8, &mlcore.Ollama{}, ""); got != 1 {
		t.Fatalf("uncanaried slots=%d want 1", got)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]string{"role": "assistant", "content": `{"ok":true}`},
		})
	}))
	t.Cleanup(srv.Close)
	hot := &mlcore.Ollama{BaseURL: srv.URL, HTTP: srv.Client()}
	if err := hot.Canary(context.Background(), "qwen3.8:27b", "sha256:hot"); err != nil {
		t.Fatal(err)
	}
	if got := claimSlotLimit([]string{"context_windows"}, 8, hot, ""); got != 1 {
		t.Fatalf("hot context slots=%d want 1", got)
	}
	if got := claimSlotLimit([]string{"transcribe"}, 2, nil, "cpu"); got != 2 {
		t.Fatalf("transcribe slots=%d want concurrent", got)
	}
	if got := claimSlotLimit([]string{"visual_index"}, 0, nil, ""); got != 4 {
		t.Fatalf("visual slots=%d want default 4", got)
	}
}

func TestMLJobOutcomeYieldAndSuccess(t *testing.T) {
	status, _, _ := mlJobOutcome(nil, 0)
	if status != "succeeded" {
		t.Fatalf("nil err status=%s", status)
	}
	status, _, _ = mlJobOutcome(vision.ErrYield, 1)
	if status != "queued" {
		t.Fatalf("yield status=%s", status)
	}
}

func TestMLJobOutcomeNoAudioSucceeded(t *testing.T) {
	err := fmt.Errorf("%w: Output file #0 does not contain any stream", errNoAudio)
	status, lastErr, retry := mlJobOutcome(err, 0)
	if status != "succeeded" {
		t.Fatalf("status=%s want succeeded", status)
	}
	if retry != 0 {
		t.Fatalf("retry=%d want 0", retry)
	}
	if lastErr == "" {
		t.Fatal("expected last_error note")
	}
	if status == "waiting_model" || status == "retry_wait" {
		t.Fatalf("mute video must not cool down or retry: %s", status)
	}
}

func TestMLJobOutcomeWaitingAssets(t *testing.T) {
	status, lastErr, retry := mlJobOutcome(fmt.Errorf("waiting_assets: video not found"), 0)
	if status != "waiting_assets" || retry != 120 {
		t.Fatalf("status=%s retry=%d", status, retry)
	}
	if lastErr == "no rows in result set" {
		t.Fatal("raw ErrNoRows leaked into last_error")
	}
}

func TestLeaseLost(t *testing.T) {
	if got := leaseLost(pgx.ErrNoRows); !errors.Is(got, mlcore.ErrSuperseded) {
		t.Fatalf("want superseded, got %v", got)
	}
	other := errors.New("db down")
	if got := leaseLost(other); !errors.Is(got, other) {
		t.Fatalf("want passthrough, got %v", got)
	}
}

func TestIdleClaimWait(t *testing.T) {
	if got := idleClaimWait(true); got != 30*time.Second {
		t.Fatalf("empty claim wait=%s want 30s", got)
	}
	if got := idleClaimWait(false); got != 3*time.Second {
		t.Fatalf("busy wait=%s want 3s", got)
	}
}
