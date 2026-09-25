package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"thirdcoast.systems/rewind/cmd/ml/internal/mlcore"
	"thirdcoast.systems/rewind/internal/application"
	"thirdcoast.systems/rewind/internal/config"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/modelruntime"
	"thirdcoast.systems/rewind/internal/osint"
	"thirdcoast.systems/rewind/internal/runtimecfg"
	"thirdcoast.systems/rewind/internal/topics"
	"thirdcoast.systems/rewind/pkg/plugin"
	"thirdcoast.systems/rewind/pkg/plugin/builtin"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Info("Starting rewind-ml worker")

	conf, err := config.LoadConfig(ctx)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	if conf.DatabaseRetries <= 0 {
		conf.DatabaseRetries = 10
	}

	downloadsDir := envOr("DOWNLOADS_DIR", "/downloads")
	primary := envOr("CONTEXT_MODEL", mlcore.DefaultContextModel)
	challengers := splitCSV(envOr("CONTEXT_MODEL_CHALLENGERS", "gemma4:26b,gemma4:12b,qwen3.5:4b"))
	ollamaHost := envOr("OLLAMA_HOST", "http://127.0.0.1:11434")

	logWhisperStartupInfo()
	ensureRuntime(ctx)

	pool, err := application.OpenDBPoolWithRetry(ctx, *conf)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	dbc, err := db.NewDatabaseConnection(ctx, pool)
	if err != nil {
		slog.Error("failed to create database connection", "error", err)
		os.Exit(1)
	}
	defer dbc.Close()
	builtin.Defaults(dbc)
	if root, ok := plugin.LocalRoot(); ok {
		downloadsDir = root
	}
	if err := runtimecfg.Start(ctx, dbc, "ml"); err != nil {
		slog.Error("live settings initialization failed", "error", err)
		return
	}

	go func() {
		if err := modelruntime.Serve(ctx, dbc); err != nil {
			slog.Error("model control server stopped", "error", err)
		}
	}()
	if err := dbc.Queries(ctx).RecoverStuckMLJobs(ctx); err != nil {
		slog.Error("failed to recover stuck ml jobs", "error", err)
	}

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = fmt.Sprintf("pid-%d", os.Getpid())
	}
	workerID := fmt.Sprintf("ml-%s", hostname)

	ollama := &mlcore.Ollama{BaseURL: ollamaHost, Model: primary}

	wake := make(chan struct{}, 1)
	wakeWorkers := func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	go db.RunListenLoop(ctx, dbc, []string{"ml_jobs"}, func(*pgconn.Notification) { wakeWorkers() }, wakeWorkers)
	go runMLMaintenance(ctx, dbc)
	go runTopicBackfill(ctx, dbc)
	go runStitchAlignmentWorker(ctx, dbc, downloadsDir, workerID+"-stitch-alignment")

	slots := runtimecfg.Int(context.TODO(), "ml.concurrent")
	if slots < 1 {
		slots = 2
	}
	// 27B KV is the scarce resource. Two 32k sequences on one 24 GiB card
	// dropped prefill to ~6 tok/s. One in-flight context job uses the GPU.
	const ollamaParallel = 1
	if slots > ollamaParallel {
		slots = ollamaParallel
	}
	maybeStartOllama(ctx, ollamaParallel)
	maybeStartVision(ctx)
	maybeStartTextcls(ctx)
	startDiarize(ctx)
	device := strings.ToLower(envOr("WHISPER_DEVICE", envOr("VISION_DEVICE", "cpu")))
	kindGroups := filterSkippedKindGroups(mlWorkerKindGroups(device), os.Getenv("ML_SKIP_KINDS"))
	for i := 0; i < slots; i++ {
		for _, kinds := range kindGroups {
			go mlWorker(ctx, dbc, fmt.Sprintf("%s-%s-%d", workerID, kinds[0], i), downloadsDir, ollama, wake, kinds)
		}
	}

	slog.Info("rewind-ml worker started",
		"worker_id", workerID,
		"downloads", downloadsDir,
		"context_model", primary,
		"challengers", challengers,
		"ollama_host", ollamaHost,
		"kind_slots", slots,
		"ollama_num_parallel", ollamaParallel,
		"device", device,
		"worker_groups", len(kindGroups),
	)

	<-ctx.Done()
	slog.Info("rewind-ml worker stopping")
}

func runTopicBackfill(ctx context.Context, dbc *db.DatabaseConnection) {
	store := topics.New(dbc)
	q := dbc.Queries(ctx)
	idle := time.NewTicker(time.Minute)
	defer idle.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		tenants, err := q.ListTopicTenants(ctx)
		if err != nil {
			slog.Warn("topic tenant enumeration", "error", err)
			tenants = nil
		}
		n := 0
		for _, tenant := range tenants {
			rowCtx := ctx
			if tenant.Valid && tenant.Bytes != [16]byte{} {
				rowCtx = plugin.WithTenantScope(ctx, tenant.String(), true)
			}
			bound, berr := store.Backfill(rowCtx, 200)
			n += bound
			if berr != nil {
				err = berr
				break
			}
		}
		if err != nil {
			slog.Warn("topic bind backfill", "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(15 * time.Second):
			}
			continue
		}
		if n > 0 {
			slog.Info("topic bind backfill", "windows", n)
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-idle.C:
		}
	}
}

func runMLMaintenance(ctx context.Context, dbc *db.DatabaseConnection) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	var lastDemand time.Time
	for {
		if ctx.Err() != nil {
			return
		}
		q := dbc.Queries(ctx)
		if err := q.RecoverStuckMLJobs(ctx); err != nil {
			slog.Error("recover ML leases", "error", err)
		}
		if err := enqueueVision(ctx, dbc); err != nil {
			slog.Warn("enqueue vision indexes", "error", err)
		}
		if err := enqueueTextcls(ctx, dbc); err != nil {
			slog.Warn("enqueue textcls jobs", "error", err)
		}
		if err := enqueueDiarizeBackfill(ctx, dbc); err != nil {
			slog.Warn("enqueue diarize jobs", "error", err)
		}
		if err := osint.RunMaintenance(ctx, dbc); err != nil {
			slog.Warn("osint maintenance", "error", err)
		}
		if lastDemand.IsZero() || time.Since(lastDemand) >= 15*time.Minute {
			if err := enqueueDemandContext(ctx, dbc); err != nil {
				slog.Warn("enqueue demand context", "error", err)
			} else {
				lastDemand = time.Now()
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func maybeStartOllama(ctx context.Context, parallel int) {
	host := envOr("OLLAMA_HOST", "http://127.0.0.1:11434")
	if !localHTTPHost(host) {
		slog.Info("OLLAMA_HOST is remote; not starting in-process ollama serve", "host", host)
		return
	}
	if _, err := exec.LookPath("ollama"); err != nil {
		slog.Info("ollama binary not on PATH; using OLLAMA_HOST only (will not pull models)")
		return
	}
	if parallel < 1 {
		parallel = 2
	}
	cmd := exec.CommandContext(ctx, "ollama", "serve")
	cmd.Env = os.Environ()
	// Many context jobs share one loaded model; NUM_PARALLEL is that in-flight cap.
	cmd.Env = append(cmd.Env, "OLLAMA_MAX_LOADED_MODELS=8", fmt.Sprintf("OLLAMA_NUM_PARALLEL=%d", parallel))
	if os.Getenv("OLLAMA_NOPRUNE") == "" {
		cmd.Env = append(cmd.Env, "OLLAMA_NOPRUNE=true")
	}
	if os.Getenv("OLLAMA_NO_CLOUD") == "" {
		cmd.Env = append(cmd.Env, "OLLAMA_NO_CLOUD=true")
	}
	if os.Getenv("OLLAMA_FLASH_ATTENTION") == "" {
		cmd.Env = append(cmd.Env, "OLLAMA_FLASH_ATTENTION=true")
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		slog.Warn("ollama serve not started", "error", err)
		return
	}
	slog.Info("ollama serve started; models are installed explicitly, worker will not pull")
	go func() {
		if err := cmd.Wait(); err != nil && ctx.Err() == nil {
			slog.Error("ollama serve exited", "error", err)
		}
	}()
}

func maybeStartVision(ctx context.Context) {
	host := envOr("VISION_URL", "http://127.0.0.1:3003")
	if !localHTTPHost(host) {
		slog.Info("VISION_URL is remote; not starting in-process vision server", "url", host)
		return
	}
	cwd := envOr("VISION_APP_DIR", "/opt/vision")
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		slog.Info("vision app dir missing; using VISION_URL only", "dir", cwd)
		return
	}
	args := []string{"app:app", "--host", "0.0.0.0", "--port", "3003", "--workers", "1"}
	python := filepath.Join(envOr("RUNTIME_DIR", defaultRuntimeDir), "vision", "bin", "python")
	var cmd *exec.Cmd
	if _, err := os.Stat(python); err == nil {
		cmd = exec.CommandContext(ctx, python, append([]string{"-m", "uvicorn"}, args...)...)
	} else if _, err := exec.LookPath("python3"); err == nil {
		cmd = exec.CommandContext(ctx, "python3", append([]string{"-m", "uvicorn"}, args...)...)
	} else if _, err := exec.LookPath("uvicorn"); err == nil {
		cmd = exec.CommandContext(ctx, "uvicorn", args...)
	} else {
		slog.Info("uvicorn/python3 not on PATH; using VISION_URL only")
		return
	}
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		slog.Warn("vision server not started", "error", err)
		return
	}
	slog.Info("vision server started", "cwd", cwd, "listen", "http://0.0.0.0:3003")
	go func() {
		if err := cmd.Wait(); err != nil && ctx.Err() == nil {
			slog.Error("vision server exited", "error", err)
		}
	}()
}

func localHTTPHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	h = strings.TrimPrefix(h, "http://")
	h = strings.TrimPrefix(h, "https://")
	return strings.HasPrefix(h, "127.0.0.1") || strings.HasPrefix(h, "localhost") || strings.HasPrefix(h, "0.0.0.0")
}

// mlWorkerKindGroups returns the per-goroutine kind slices. CUDA/ROCm share one
// GPU with Ollama, so GPU kinds share a single worker; textcls stays on a
// separate CPU group that never takes the Ollama lock. CPU keeps the split.
func mlWorkerKindGroups(device string) [][]string {
	textclsKinds := []string{"comment_classify", "speech_tone"}
	switch strings.ToLower(strings.TrimSpace(device)) {
	case "cuda", "rocm":
		return [][]string{
			{"visual_index", "transcribe", "context_windows", "refine_boundaries", "diarize"},
			textclsKinds,
		}
	default:
		return [][]string{
			{"visual_index"},
			{"transcribe"},
			{"context_windows", "refine_boundaries", "diarize"},
			textclsKinds,
		}
	}
}

// filterSkippedKindGroups removes kinds named in skipCSV (comma-separated,
// spaces trimmed, empty entries ignored). A blank list returns groups
// unchanged. Groups left with no kinds are omitted so they are not started.
func filterSkippedKindGroups(groups [][]string, skipCSV string) [][]string {
	skip := splitCSV(skipCSV)
	if len(skip) == 0 {
		return groups
	}
	drop := make(map[string]struct{}, len(skip))
	for _, name := range skip {
		drop[name] = struct{}{}
	}
	out := make([][]string, 0, len(groups))
	for _, group := range groups {
		kept := make([]string, 0, len(group))
		removed := false
		for _, kind := range group {
			if _, ok := drop[kind]; ok {
				removed = true
				continue
			}
			kept = append(kept, kind)
		}
		if len(kept) == 0 {
			continue
		}
		if !removed {
			out = append(out, group)
			continue
		}
		out = append(out, kept)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func maybeStartTextcls(ctx context.Context) {
	host := envOr("TEXTCLS_URL", "http://127.0.0.1:3004")
	if !localHTTPHost(host) {
		slog.Info("TEXTCLS_URL is remote; not starting in-process textcls server", "url", host)
		return
	}
	cwd := envOr("TEXTCLS_APP_DIR", "/opt/textcls")
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		slog.Info("textcls app dir missing; using TEXTCLS_URL only", "dir", cwd)
		return
	}
	args := []string{"app:app", "--host", "0.0.0.0", "--port", "3004", "--workers", "1"}
	python := filepath.Join(envOr("RUNTIME_DIR", defaultRuntimeDir), "textcls", "bin", "python")
	var cmd *exec.Cmd
	if _, err := os.Stat(python); err == nil {
		cmd = exec.CommandContext(ctx, python, append([]string{"-m", "uvicorn"}, args...)...)
	} else if _, err := exec.LookPath("python3"); err == nil {
		cmd = exec.CommandContext(ctx, "python3", append([]string{"-m", "uvicorn"}, args...)...)
	} else if _, err := exec.LookPath("uvicorn"); err == nil {
		cmd = exec.CommandContext(ctx, "uvicorn", args...)
	} else {
		slog.Info("uvicorn/python3 not on PATH; using TEXTCLS_URL only")
		return
	}
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		slog.Warn("textcls server not started", "error", err)
		return
	}
	slog.Info("textcls server started", "cwd", cwd, "listen", "http://0.0.0.0:3004")
	go func() {
		if err := cmd.Wait(); err != nil && ctx.Err() == nil {
			slog.Error("textcls server exited", "error", err)
		}
	}()
}
