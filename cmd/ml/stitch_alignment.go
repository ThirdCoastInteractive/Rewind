package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgtype"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/stitch"
	"time"
)

type alignmentRuntimeResult struct {
	State string           `json:"state"`
	Words []map[string]any `json:"words"`
	Error string           `json:"error,omitempty"`
}

// runStitchAlignmentWorker processes the bounded Stitch alignment queue. The
// polling loop also recovers jobs whose worker lease expired.
func runStitchAlignmentWorker(ctx context.Context, dbc *db.DatabaseConnection, downloadsDir, workerID string) {
	// Alignment dependencies can be large and must never delay whisper.cpp/ASR
	// startup. Install them only in this dedicated worker, with cancellation.
	runtimeDir := envOr("RUNTIME_DIR", defaultRuntimeDir)
	installCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	installErr := ensureAlignment(installCtx, runtimeDir)
	if installErr != nil {
		slog.Warn("stitch alignment runtime unavailable", "error", installErr)
	}
	cancel()
	store := stitch.NewStore(dbc)
	err := stitch.RunAlignmentWorker(ctx, dbc, workerID, 2*time.Second,
		func(ctx context.Context, claim *stitch.AlignmentClaim) (any, error) {
			if installErr != nil {
				return alignmentRuntimeResult{State: "missing_model", Error: installErr.Error()}, nil
			}
			return runStitchAlignment(ctx, dbc, claim, downloadsDir)
		},
		func(ctx context.Context, claim *stitch.AlignmentClaim, result any) error {
			r, ok := result.(alignmentRuntimeResult)
			if !ok {
				return fmt.Errorf("unexpected alignment result %T", result)
			}
			b, err := json.Marshal(r)
			if err != nil {
				return err
			}
			_, err = store.ApplyAlignmentResult(ctx, *claim, b)
			return err
		})
	if err != nil && ctx.Err() == nil {
		slog.Error("stitch alignment worker stopped", "error", err)
	}
}

// runStitchAlignment extracts only the requested source range and invokes WhisperX forced alignment.
func runStitchAlignment(ctx context.Context, dbc *db.DatabaseConnection, claim *stitch.AlignmentClaim, downloadsDir string) (alignmentRuntimeResult, error) {
	if claim == nil || claim.SourceStartUS < 0 || claim.SourceEndUS <= claim.SourceStartUS || claim.SourceEndUS-claim.SourceStartUS > 120_000_000 || claim.EndUS <= claim.StartUS || claim.EndUS-claim.StartUS > 120_000_000 {
		return alignmentRuntimeResult{State: "unalignable", Error: "invalid range"}, nil
	}
	var vid pgtype.UUID
	e := vid.Scan(claim.SourceVideoID)
	if e != nil {
		return alignmentRuntimeResult{State: "error", Error: e.Error()}, e
	}
	v, e := dbc.Queries(ctx).GetVideoByID(ctx, vid)
	if e != nil {
		return alignmentRuntimeResult{State: "error", Error: e.Error()}, e
	}
	path := resolveVideoPath(v, downloadsDir)
	if path == "" {
		return alignmentRuntimeResult{State: "unalignable", Error: "source unavailable"}, nil
	}
	tmp, e := os.MkdirTemp("", "rewind-align-")
	if e != nil {
		return alignmentRuntimeResult{}, e
	}
	defer os.RemoveAll(tmp)
	wav := filepath.Join(tmp, "audio.wav")
	if e = extractWav16k(ctx, path, wav, float64(claim.SourceStartUS)/1e6, float64(claim.SourceEndUS)/1e6); e != nil {
		return alignmentRuntimeResult{State: "error", Error: e.Error()}, e
	}
	req, _ := json.Marshal(map[string]any{"audio_path": wav, "text": claim.Text, "language": claim.Language, "start": 0, "end": float64(claim.SourceEndUS-claim.SourceStartUS) / 1e6, "version": claim.ModelVersion})
	py := filepath.Join(envOr("RUNTIME_DIR", "/runtime"), "alignment", "bin", "python")
	cmd := exec.CommandContext(ctx, py, "/opt/alignment/app.py")
	cmd.Stdin = bytes.NewReader(req)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if e = cmd.Run(); e != nil {
		return alignmentRuntimeResult{State: "error", Error: strings.TrimSpace(stderr.String())}, e
	}
	var result alignmentRuntimeResult
	if e = json.Unmarshal(out.Bytes(), &result); e != nil {
		return alignmentRuntimeResult{State: "error", Error: fmt.Sprintf("alignment output: %v", e)}, e
	}
	return result, nil
}
