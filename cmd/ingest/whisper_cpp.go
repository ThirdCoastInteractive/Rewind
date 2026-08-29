package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultWhisperModel    = "small"
	defaultWhisperModelDir = "/models"
	defaultWhisperCmd      = "whisper-cli"
	ggmlHFBase             = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/"
)

var modelNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

var modelFetchMu sync.Mutex

type whisperConfig struct {
	Enabled   bool
	Cmd       string
	Model     string
	ModelDir  string
	ModelURL  string
	Language  string
	Task      string
	Translate bool
	Device    string
	Extra     []string
	Timeout   time.Duration
}

func loadWhisperConfig() whisperConfig {
	cfg := whisperConfig{
		Enabled:  whisperEnabled(),
		Cmd:      envOr("WHISPER_CMD", defaultWhisperCmd),
		Model:    envOr("WHISPER_MODEL", defaultWhisperModel),
		ModelDir: envOr("WHISPER_MODEL_DIR", defaultWhisperModelDir),
		ModelURL: strings.TrimSpace(os.Getenv("WHISPER_MODEL_URL")),
		Language: strings.TrimSpace(os.Getenv("WHISPER_LANGUAGE")),
		Task:     envOr("WHISPER_TASK", "transcribe"),
		Device:   envOr("WHISPER_DEVICE", "cpu"),
	}
	cfg.Translate = strings.EqualFold(cfg.Task, "translate")
	if extra := strings.TrimSpace(os.Getenv("WHISPER_ARGS")); extra != "" {
		cfg.Extra = strings.Fields(extra)
	}
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv("WHISPER_TIMEOUT_SECONDS"))); err == nil && n > 0 {
		cfg.Timeout = time.Duration(n) * time.Second
	}
	return cfg
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// ggmlBinName maps WHISPER_MODEL (small, large-v3, large-v3-turbo, …) to a
// ggml-*.bin filename. A value that already looks like a ggml file is kept.
func ggmlBinName(model string) (string, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		model = defaultWhisperModel
	}
	if strings.Contains(model, "..") || strings.ContainsAny(model, `/\`) {
		return "", fmt.Errorf("whisper: invalid model name")
	}
	base := filepath.Base(model)
	if strings.HasPrefix(base, "ggml-") && strings.HasSuffix(base, ".bin") {
		stem := strings.TrimSuffix(strings.TrimPrefix(base, "ggml-"), ".bin")
		if !modelNameRe.MatchString(stem) {
			return "", fmt.Errorf("whisper: invalid model name %q", model)
		}
		return base, nil
	}
	if !modelNameRe.MatchString(base) {
		return "", fmt.Errorf("whisper: invalid model name %q", model)
	}
	return "ggml-" + base + ".bin", nil
}

func whisperModelPath(cfg whisperConfig) (string, error) {
	name, err := ggmlBinName(cfg.Model)
	if err != nil {
		return "", err
	}
	return filepath.Join(cfg.ModelDir, name), nil
}

func whisperDownloadURL(cfg whisperConfig, binName string) string {
	if cfg.ModelURL != "" {
		return cfg.ModelURL
	}
	return ggmlHFBase + binName
}

// whisperCLIArgs builds whisper.cpp flags: -m model, -f wav, -ovtt, -of prefix,
// optional -l language, and -tr when translating to English.
func whisperCLIArgs(modelPath, wavPath, outPrefix string, cfg whisperConfig) []string {
	args := []string{
		"-m", modelPath,
		"-f", wavPath,
		"-ovtt",
		"-of", outPrefix,
		"-np",
	}
	if cfg.Language != "" && !strings.EqualFold(cfg.Language, "auto") {
		args = append(args, "-l", cfg.Language)
	}
	if cfg.Translate {
		args = append(args, "-tr")
	}
	args = append(args, cfg.Extra...)
	return args
}

func ensureWhisperModel(ctx context.Context, cfg whisperConfig) (string, error) {
	path, err := whisperModelPath(cfg)
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(path); err == nil && st.Size() > 0 {
		return path, nil
	}
	modelFetchMu.Lock()
	defer modelFetchMu.Unlock()
	if st, err := os.Stat(path); err == nil && st.Size() > 0 {
		return path, nil
	}
	if err := os.MkdirAll(cfg.ModelDir, 0o755); err != nil {
		return "", fmt.Errorf("whisper: create model dir: %w", err)
	}
	binName, _ := ggmlBinName(cfg.Model)
	url := whisperDownloadURL(cfg, binName)
	slog.Info("whisper model missing; downloading", "model", cfg.Model, "url", url, "dest", path)
	if err := downloadFile(ctx, url, path); err != nil {
		return "", fmt.Errorf("whisper: download %s: %w", cfg.Model, err)
	}
	return path, nil
}

func downloadFile(ctx context.Context, url, dest string) error {
	tmp := dest + ".tmp"
	_ = os.Remove(tmp)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "rewind-ingest/whisper.cpp")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func extractWav16k(ctx context.Context, videoPath, wavPath string) error {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return fmt.Errorf("whisper: ffmpeg not found: %w", err)
	}
	cmd := exec.CommandContext(ctx, ffmpeg,
		"-y", "-i", videoPath,
		"-vn", "-ac", "1", "-ar", "16000",
		"-c:a", "pcm_s16le",
		wavPath,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("whisper: extract wav: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}
