package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"thirdcoast.systems/rewind/cmd/ml/internal/mlcore"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/modelruntime"
	"thirdcoast.systems/rewind/internal/runtimecfg"
	"thirdcoast.systems/rewind/internal/vision"
)

// contextLoader serializes the first context claim until Ollama has canaried.
// ClaimMLJob's SlotLimit count races across workers, so SQL alone cannot keep
// a cold start to one loader.
var contextLoader sync.Mutex

func mlWorker(ctx context.Context, dbc *db.DatabaseConnection, workerID, downloadsDir string, ollama *mlcore.Ollama, wake <-chan struct{}, kinds []string) {
	q := dbc.Queries(ctx)
	for {
		if ctx.Err() != nil {
			return
		}
		emptyClaim := false
		for {
			if !transcribeRuntimeReady(ctx, q, kinds) {
				break
			}
			heldLoader := false
			if wantsKind(kinds, "context_windows") && (ollama == nil || !ollama.Resident()) {
				if !contextLoader.TryLock() {
					break
				}
				heldLoader = true
				if ollama != nil && ollama.Resident() {
					contextLoader.Unlock()
					heldLoader = false
				}
			}
			slots := claimSlotLimit(kinds, int32(runtimecfg.Int(ctx, "ml.concurrent")), ollama, runtimecfg.Env(ctx, "WHISPER_DEVICE"))
			job, err := q.ClaimMLJob(ctx, &db.ClaimMLJobParams{WorkerID: workerID, Kinds: kinds, SlotLimit: slots})
			if err != nil {
				if heldLoader {
					contextLoader.Unlock()
				}
				if errors.Is(err, pgx.ErrNoRows) {
					emptyClaim = true
				} else {
					slog.Error("claim ml job", "error", err)
				}
				break
			}
			if job == nil {
				if heldLoader {
					contextLoader.Unlock()
				}
				emptyClaim = true
				break
			}
			err = processMLJob(ctx, dbc, job, downloadsDir, ollama)
			if heldLoader {
				contextLoader.Unlock()
			}
			if errors.Is(err, mlcore.ErrInfrastructure) || (job.Kind == "transcribe" && mlcore.IsWaiting(err)) {
				// First 27B load often 500s; cooling the whole kind parks thousands of jobs.
				if job.Kind != "context_windows" || ollama.Resident() {
					ollama.VerifiedDigest = ""
					coolDownKind(ctx, q, job.Kind, err.Error())
				}
			} else if err == nil {
				if e := q.RecordMLRuntimeSuccess(ctx, job.Kind); e != nil {
					slog.Error("record runtime recovery", "error", e)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-wake:
		case <-time.After(idleClaimWait(emptyClaim)):
		}
	}
}

func mlJobSkipsOllama(kind string) bool {
	switch kind {
	case "context_windows", "visual_index", "refine_boundaries", "comment_classify", "speech_tone", "diarize":
		return true
	default:
		return false
	}
}

func idleClaimWait(emptyClaim bool) time.Duration {
	if emptyClaim {
		return 30 * time.Second
	}
	return 3 * time.Second
}

func processMLJob(ctx context.Context, dbc *db.DatabaseConnection, job *db.MlJob, downloadsDir string, ollama *mlcore.Ollama) error {
	var captureErr error
	ctx, captureErr = runtimecfg.Job(ctx, dbc, "ml", job.ID)
	if captureErr != nil {
		return captureErr
	}
	primary := runtimecfg.String(ctx, "ml.context_model")
	challengers := splitCSV(runtimecfg.String(ctx, "ml.challengers"))
	ollama = ollama.ForJob(primary)
	q := dbc.Queries(ctx)
	slog.Info("ml job claimed", "id", uuidString(job.ID), "kind", job.Kind, "video_id", uuidString(job.VideoID), "attempts", job.Attempts)
	hctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go heartbeatML(hctx, q, job, cancel)

	var err error
	model := primary
	switch job.Kind {
	case "transcribe":
		model = runtimecfg.String(ctx, "whisper.model")
	case "visual_index":
		model = runtimecfg.String(ctx, "vision.clip_model")
	case "comment_classify", "speech_tone":
		model = runtimecfg.String(ctx, "textcls.sentiment_model")
	case "diarize":
		model = runtimecfg.String(ctx, "diarize.model")
	}
	if job.Kind == "transcribe" {
		err = probeWhisperCLI(hctx, loadWhisperConfig(hctx))
	}
	if err == nil {
		var release func()
		var leaseErr error
		if mlJobSkipsOllama(job.Kind) {
			release = func() {}
		} else {
			release, leaseErr = modelruntime.Acquire(hctx, model)
		}
		if leaseErr != nil {
			err = fmt.Errorf("waiting_model: waiting_capacity")
		} else {
			defer release()
			switch job.Kind {
			case "visual_index":
				err = handleVision(hctx, dbc, job)
			case "transcribe":
				err = handleTranscribe(hctx, dbc, job, downloadsDir)
			case "context_windows":
				err = handleContextWindows(hctx, dbc, job, ollama, primary, challengers)
			case "refine_boundaries":
				err = handleRefineBoundaries(hctx, dbc, job)
			case "comment_classify":
				err = handleCommentClassify(hctx, dbc, job)
			case "speech_tone":
				err = handleSpeechTone(hctx, dbc, job)
			case "diarize":
				err = handleDiarize(hctx, dbc, job, downloadsDir)
			default:
				err = fmt.Errorf("unknown ml job kind %q", job.Kind)
			}
		}
	}
	cancel()

	status, lastErr, retrySeconds := mlJobOutcome(err, job.FailureCount)
	switch status {
	case "succeeded":
		slog.Info("ml job succeeded", "id", uuidString(job.ID), "kind", job.Kind)
	case "superseded":
		slog.Info("ml job superseded", "id", uuidString(job.ID), "kind", job.Kind)
	case "waiting_model", "waiting_assets":
		slog.Warn("ml job "+status, "id", uuidString(job.ID), "kind", job.Kind, "error", err)
	case "failed", "retry_wait":
		slog.Error("ml job failed", "id", uuidString(job.ID), "kind", job.Kind, "error", err)
	}
	if ferr := q.FinishMLJob(ctx, &db.FinishMLJobParams{
		ID:            job.ID,
		Status:        status,
		LastError:     lastErr,
		ModelDigest:   "",
		PromptVersion: "",
		LeaseToken:    job.LeaseToken,
		RetrySeconds:  retrySeconds,
	}); ferr != nil {
		slog.Error("finish ml job", "id", uuidString(job.ID), "error", ferr)
	}
	return err
}

func mlJobOutcome(err error, failureCount int32) (status, lastErr string, retrySeconds int32) {
	switch {
	case err == nil:
		return "succeeded", "", 0
	case errors.Is(err, errNoAudio):
		return "succeeded", err.Error(), 0
	case errors.Is(err, vision.ErrYield):
		return "queued", "", 0
	case errors.Is(err, mlcore.ErrSuperseded):
		return "superseded", "", 0
	case err != nil && strings.Contains(err.Error(), "waiting_assets"):
		return "waiting_assets", err.Error(), 120
	case errors.Is(err, mlcore.ErrInfrastructure):
		// Kind-level ml_runtime_health cooldown is the retry gate.
		return "waiting_model", err.Error(), 0
	case mlcore.IsWaiting(err):
		return "waiting_model", err.Error(), 120
	default:
		if failureCount < 3 {
			return "retry_wait", err.Error(), []int32{60, 300, 900}[failureCount]
		}
		return "failed", err.Error(), 0
	}
}

func claimSlotLimit(kinds []string, concurrent int32, ollama *mlcore.Ollama, whisperDevice string) int32 {
	if concurrent < 1 {
		concurrent = 4
	}
	gpu := strings.EqualFold(whisperDevice, "cuda") || strings.EqualFold(whisperDevice, "rocm")
	if gpu && len(kinds) > 1 {
		// One shared CUDA/ROCm worker must not claim whisper + 27B together.
		return 1
	}
	if wantsKind(kinds, "context_windows") || wantsKind(kinds, "refine_boundaries") {
		// One 27B sequence. A second claim shares no useful parallelism and
		// halves prefill on a 24 GiB card.
		return 1
	}
	if wantsKind(kinds, "transcribe") && gpu {
		// Two CUDA Whisper runs pin the desktop GPU and stall NVDEC.
		return 1
	}
	return concurrent
}

func leaseLost(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return mlcore.ErrSuperseded
	}
	return err
}

func wantsKind(kinds []string, kind string) bool {
	for _, k := range kinds {
		if k == kind {
			return true
		}
	}
	return false
}

func kindCoolingDown(ctx context.Context, q *db.Queries, kind string) bool {
	rows, err := q.ListMLRuntimeHealth(ctx)
	if err != nil {
		return false
	}
	now := time.Now()
	for _, h := range rows {
		if h != nil && h.Kind == kind && h.RetryAt.Valid && h.RetryAt.Time.After(now) {
			return true
		}
	}
	return false
}

func coolDownKind(ctx context.Context, q *db.Queries, kind, lastErr string) {
	if e := q.RecordMLRuntimeFailure(ctx, &db.RecordMLRuntimeFailureParams{Kind: kind, LastError: lastErr}); e != nil {
		slog.Error("record runtime failure", "kind", kind, "error", e)
	}
	if e := q.CooldownMLKind(ctx, kind); e != nil {
		slog.Error("cooldown ml kind", "kind", kind, "error", e)
	}
}

var lastWhisperProbeOK time.Time

// transcribeRuntimeReady is true when this worker is not responsible for
// transcribe, or whisper-cli can actually start. A CUDA-linked binary
// without libcuda must not be claimed (WAV extract would run first).
func transcribeRuntimeReady(ctx context.Context, q *db.Queries, kinds []string) bool {
	if !wantsKind(kinds, "transcribe") {
		return true
	}
	if kindCoolingDown(ctx, q, "transcribe") {
		return false
	}
	if !lastWhisperProbeOK.IsZero() && time.Since(lastWhisperProbeOK) < 30*time.Second {
		return true
	}
	if err := probeWhisperCLI(ctx, loadWhisperConfig(ctx)); err != nil {
		slog.Warn("whisper-cli not runnable; skipping transcribe claims", "error", err)
		coolDownKind(ctx, q, "transcribe", err.Error())
		return false
	}
	if e := q.RecordMLRuntimeSuccess(ctx, "transcribe"); e != nil {
		slog.Error("record whisper probe recovery", "error", e)
	}
	lastWhisperProbeOK = time.Now()
	return true
}

func heartbeatML(ctx context.Context, q *db.Queries, job *db.MlJob, cancel context.CancelFunc) {
	t := time.NewTicker(45 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := q.HeartbeatMLJob(ctx, &db.HeartbeatMLJobParams{ID: job.ID, LeaseToken: job.LeaseToken}); err != nil || n == 0 {
				slog.Warn("ml heartbeat lost", "id", uuidString(job.ID), "error", err)
				cancel()
				return
			}
		}
	}
}
