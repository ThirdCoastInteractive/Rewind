package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/cmd/ml/internal/mlcore"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/diarize"
	"thirdcoast.systems/rewind/internal/runtimecfg"
	"thirdcoast.systems/rewind/pkg/plugin"
)

// startDiarize brings the sidecar up only when diarize.model is nemotron-3.
// The default stays off, so a normal boot does not install Torch.
func startDiarize(ctx context.Context) {
	if !diarizeEnabled(ctx) {
		slog.Info("diarize disabled", "model", runtimecfg.String(ctx, "diarize.model"))
		return
	}
	go func() {
		dir := envOr("RUNTIME_DIR", defaultRuntimeDir)
		device := runtimecfg.String(ctx, "diarize.device")
		if err := ensureDiarize(ctx, dir, device); err != nil {
			slog.Warn("diarize runtime", "error", err)
			return
		}
		maybeStartDiarize(ctx, device)
		installDiarizeWeights(ctx)
	}()
}

func maybeStartDiarize(ctx context.Context, device string) {
	host := envOr("DIARIZE_URL", "http://127.0.0.1:3005")
	if !localHTTPHost(host) {
		slog.Info("DIARIZE_URL is remote; not starting in-process diarize server", "url", host)
		return
	}
	cwd := envOr("DIARIZE_APP_DIR", "/opt/diarize")
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		slog.Info("diarize app dir missing; using DIARIZE_URL only", "dir", cwd)
		return
	}
	args := []string{"app:app", "--host", "0.0.0.0", "--port", "3005", "--workers", "1"}
	python := filepath.Join(envOr("RUNTIME_DIR", defaultRuntimeDir), "diarize", "bin", "python")
	var cmd *exec.Cmd
	if _, err := os.Stat(python); err == nil {
		cmd = exec.CommandContext(ctx, python, append([]string{"-m", "uvicorn"}, args...)...)
	} else if _, err := exec.LookPath("python3"); err == nil {
		cmd = exec.CommandContext(ctx, "python3", append([]string{"-m", "uvicorn"}, args...)...)
	} else {
		slog.Info("diarize python not available; using DIARIZE_URL only")
		return
	}
	cmd.Dir = cwd
	env := make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "DIARIZE_DEVICE=") {
			continue
		}
		env = append(env, entry)
	}
	cmd.Env = append(env, "DIARIZE_DEVICE="+device)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		slog.Warn("diarize server not started", "error", err)
		return
	}
	slog.Info("diarize server started", "cwd", cwd, "listen", host, "device", device)
	go func() {
		if err := cmd.Wait(); err != nil && ctx.Err() == nil {
			slog.Error("diarize server exited", "error", err)
		}
	}()
}

func installDiarizeWeights(ctx context.Context) {
	c := diarize.FromEnv()
	if c.URL == "" {
		return
	}
	deadline := time.Now().Add(2 * time.Minute)
	for {
		if ctx.Err() != nil {
			return
		}
		if err := c.Health(ctx); err == nil {
			break
		}
		if time.Now().After(deadline) {
			slog.Warn("diarize sidecar did not become healthy; weights were not installed")
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
	models, err := c.Models(ctx)
	if err != nil {
		slog.Warn("diarize models", "error", err)
		return
	}
	if _, ok := diarize.Ready(models.Models, diarize.ModelNemotron3); ok {
		return
	}
	slog.Info("installing nemotron-3 weights")
	installCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	if err := c.Install(installCtx, diarize.ModelNemotron3); err != nil {
		slog.Warn("diarize install", "error", err)
	}
}

func diarizeEnabled(ctx context.Context) bool {
	return runtimecfg.String(ctx, "diarize.model") == diarize.ModelNemotron3
}

func maybeEnqueueDiarize(ctx context.Context, dbc *db.DatabaseConnection, videoID pgtype.UUID) error {
	if !diarizeEnabled(ctx) {
		return nil
	}
	q := dbc.Queries(ctx)
	hash, err := q.GetTranscriptFingerprint(ctx, videoID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	digest := diarizeDigest(ctx)
	return plugin.Enqueue(ctx, plugin.Job{
		VideoID:        uuid.UUID(videoID.Bytes).String(),
		Kind:           plugin.KindDiarize,
		Priority:       diarize.Priority,
		TranscriptHash: hash,
		ModelDigest:    digest,
		PromptVersion:  diarize.PromptVersion,
	})
}

func diarizeDigest(ctx context.Context) string {
	c := diarize.FromEnv()
	if c.URL == "" {
		return diarize.ModelNemotron3
	}
	models, err := c.Models(ctx)
	if err != nil {
		return diarize.ModelNemotron3
	}
	if fingerprint, ok := diarize.Ready(models.Models, diarize.ModelNemotron3); ok {
		return fingerprint
	}
	return diarize.ModelNemotron3
}

func enqueueDiarizeBackfill(ctx context.Context, dbc *db.DatabaseConnection) error {
	if !diarizeEnabled(ctx) || runtimecfg.String(ctx, "diarize.backfill") != "true" {
		return nil
	}
	c := diarize.FromEnv()
	if c.URL == "" {
		return nil
	}
	models, err := c.Models(ctx)
	if err != nil {
		return nil
	}
	fingerprint, ok := diarize.Ready(models.Models, diarize.ModelNemotron3)
	if !ok {
		return nil
	}
	q := dbc.Queries(ctx)
	videos, err := q.ListVideosNeedingDiarize(ctx, &db.ListVideosNeedingDiarizeParams{
		Fingerprint: fingerprint,
		RowLimit:    50,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	for _, videoID := range videos {
		hash, herr := q.GetTranscriptFingerprint(ctx, videoID)
		if herr != nil {
			continue
		}
		if e := plugin.Enqueue(ctx, plugin.Job{
			VideoID:        uuid.UUID(videoID.Bytes).String(),
			Kind:           plugin.KindDiarize,
			Priority:       diarize.Priority,
			TranscriptHash: hash,
			ModelDigest:    fingerprint,
			PromptVersion:  diarize.PromptVersion,
		}); e != nil {
			return e
		}
	}
	return nil
}

func handleDiarize(ctx context.Context, dbc *db.DatabaseConnection, job *db.MlJob, downloadsDir string) error {
	if !diarizeEnabled(ctx) {
		return nil
	}
	c := diarize.FromEnv()
	if c.URL == "" {
		return fmt.Errorf("%w: DIARIZE_URL is not configured", mlcore.ErrWaitingModel)
	}
	q := dbc.Queries(ctx)
	hash, err := q.GetTranscriptFingerprint(ctx, job.VideoID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("waiting_assets: transcript fingerprint missing")
		}
		return err
	}
	if job.TranscriptHash != "" && job.TranscriptHash != hash {
		return mlcore.ErrSuperseded
	}
	models, err := c.Models(ctx)
	if err != nil {
		return fmt.Errorf("%w: %v", mlcore.ErrWaitingModel, err)
	}
	fingerprint, ok := diarize.Ready(models.Models, diarize.ModelNemotron3)
	if !ok {
		return fmt.Errorf("%w: nemotron-3 is not installed", mlcore.ErrWaitingModel)
	}
	video, err := q.GetVideoByID(ctx, job.VideoID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("waiting_assets: video not found")
		}
		return err
	}
	videoPath, cleanup := resolveVideoPath(ctx, video, downloadsDir)
	if cleanup != nil {
		defer cleanup()
	}
	if videoPath == "" {
		return fmt.Errorf("waiting_assets: no video file")
	}
	wav, err := os.CreateTemp("", "rewind-diarize-*.wav")
	if err != nil {
		return err
	}
	wavPath := wav.Name()
	_ = wav.Close()
	defer os.Remove(wavPath)
	if err = extractWav16k(ctx, videoPath, wavPath); err != nil {
		if errors.Is(err, errNoAudio) {
			return saveSpeakerTurns(ctx, q, job.VideoID, fingerprint, nil)
		}
		return err
	}
	device := runtimecfg.String(ctx, "diarize.device")
	turns, gotFP, err := c.Diarize(ctx, wavPath, diarize.ModelNemotron3, device)
	if err != nil {
		if strings.Contains(err.Error(), "waiting_model") {
			return fmt.Errorf("%w: %v", mlcore.ErrWaitingModel, err)
		}
		return err
	}
	if gotFP != "" {
		fingerprint = gotFP
	}
	return saveSpeakerTurns(ctx, q, job.VideoID, fingerprint, turns)
}

func saveSpeakerTurns(ctx context.Context, q *db.Queries, videoID pgtype.UUID, fingerprint string, turns []diarize.Turn) error {
	if turns == nil {
		turns = []diarize.Turn{}
	}
	raw, err := json.Marshal(turns)
	if err != nil {
		return err
	}
	return q.UpsertSpeakerTurns(ctx, &db.UpsertSpeakerTurnsParams{
		VideoID:     videoID,
		Model:       diarize.ModelNemotron3,
		Fingerprint: fingerprint,
		Turns:       raw,
	})
}
