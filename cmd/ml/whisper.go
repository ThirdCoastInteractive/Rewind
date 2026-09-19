package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"thirdcoast.systems/rewind/cmd/ml/internal/mlcore"
	"thirdcoast.systems/rewind/internal/runtimecfg"
)

const (
	defaultWhisperModel    = "large-v3-turbo"
	defaultWhisperModelDir = "/models/whisper"
	defaultWhisperCmd      = "whisper-cli"
)

var modelNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// errNoAudio is a terminal skip for mute / video-only files (no audio stream).
var errNoAudio = errors.New("no audio stream")

type whisperConfig struct {
	Cmd       string
	Model     string
	ModelDir  string
	Language  string
	Task      string
	Translate bool
	Device    string
	Extra     []string
	Timeout   time.Duration
}

func loadWhisperConfig(contexts ...context.Context) whisperConfig {
	var ctx context.Context
	if len(contexts) > 0 {
		ctx = contexts[0]
	}
	cfg := whisperConfig{
		Cmd:      envOr("WHISPER_CMD", defaultWhisperCmd),
		Model:    runtimecfg.Env(ctx, "WHISPER_MODEL"),
		ModelDir: envOr("WHISPER_MODEL_DIR", defaultWhisperModelDir),
		Language: strings.TrimSpace(runtimecfg.Env(ctx, "WHISPER_LANGUAGE")),
		Task:     runtimecfg.Env(ctx, "WHISPER_TASK"),
		Device:   runtimecfg.Env(ctx, "WHISPER_DEVICE"),
	}
	cfg.Translate = strings.EqualFold(cfg.Task, "translate")
	if extra := strings.TrimSpace(os.Getenv("WHISPER_ARGS")); extra != "" {
		cfg.Extra = strings.Fields(extra)
	}
	if n, err := strconv.Atoi(strings.TrimSpace(runtimecfg.Env(ctx, "WHISPER_TIMEOUT_SECONDS"))); err == nil && n > 0 {
		cfg.Timeout = time.Duration(n) * time.Second
	}
	return cfg
}

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

// requireWhisperModel stats the GGML file. Missing → waiting_model. Never downloads.
func requireWhisperModel(cfg whisperConfig) (string, error) {
	path, err := whisperModelPath(cfg)
	if err != nil {
		return "", fmt.Errorf("%w: %v", mlcore.ErrWaitingModel, err)
	}
	if mlcore.ModelCheck(path) == mlcore.StatusWaitingModel {
		return "", fmt.Errorf("%w: whisper model not found at %s (install explicitly; worker will not download)", mlcore.ErrWaitingModel, path)
	}
	return path, nil
}

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
	if strings.EqualFold(cfg.Device, "cpu") {
		args = append(args, "-ng")
	}
	args = append(args, cfg.Extra...)
	return args
}

func extractWav16k(ctx context.Context, videoPath, wavPath string, bounds ...float64) error {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return fmt.Errorf("whisper: ffmpeg not found: %w", err)
	}
	args := []string{"-hide_banner", "-y", "-hwaccel", "none", "-threads", "2"}
	if len(bounds) == 2 {
		args = append(args, "-ss", strconv.FormatFloat(bounds[0], 'f', 3, 64))
	}
	args = append(args, "-i", videoPath)
	if len(bounds) == 2 {
		args = append(args, "-t", strconv.FormatFloat(bounds[1]-bounds[0], 'f', 3, 64))
	}
	args = append(args, "-vn", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", wavPath)
	cmd := exec.CommandContext(ctx, ffmpeg, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if ffmpegNoAudioOutput(msg) {
			return fmt.Errorf("%w: %s", errNoAudio, msg)
		}
		return fmt.Errorf("whisper: extract wav: %w (%s)", err, msg)
	}
	return nil
}

func ffmpegNoAudioOutput(msg string) bool {
	low := strings.ToLower(msg)
	return strings.Contains(low, "does not contain any stream") ||
		strings.Contains(low, "output file #0 does not contain any stream") ||
		strings.Contains(low, "stream map '0:a") ||
		strings.Contains(low, "matches no streams")
}

func generateCaptionsWithWhisperConfig(ctx context.Context, videoPath, videoID, outputDir string, cfg whisperConfig, bounds ...float64) (string, string, error) {
	videoPath = strings.TrimSpace(videoPath)
	videoID = strings.TrimSpace(videoID)
	outputDir = strings.TrimSpace(outputDir)
	if videoPath == "" || videoID == "" || outputDir == "" {
		return "", "", fmt.Errorf("whisper: missing inputs")
	}
	if err := probeWhisperCLI(ctx, cfg); err != nil {
		return "", "", err
	}
	modelPath, err := requireWhisperModel(cfg)
	if err != nil {
		return "", "", err
	}

	langTag := "und"
	if cfg.Translate {
		langTag = "en"
	} else if cfg.Language != "" && !strings.EqualFold(cfg.Language, "auto") {
		langTag = cfg.Language
	}

	ctxToUse := ctx
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctxToUse, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}

	wavPath := filepath.Join(outputDir, videoID+".whisper.wav")
	defer os.Remove(wavPath)
	if err := extractWav16k(ctxToUse, videoPath, wavPath, bounds...); err != nil {
		return "", "", err
	}

	outPrefix := filepath.Join(outputDir, videoID+".whisper")
	args := whisperCLIArgs(modelPath, wavPath, outPrefix, cfg)
	var buf bytes.Buffer
	cmd := exec.CommandContext(ctxToUse, cfg.Cmd, args...)
	cmd.Env = whisperLibraryEnv()
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return "", "", classifyWhisperFailure(err, buf.String())
	}

	cand := outPrefix + ".vtt"
	if _, err := os.Stat(cand); err != nil {
		matches, _ := filepath.Glob(outPrefix + "*.vtt")
		if len(matches) == 0 {
			return "", "", fmt.Errorf("whisper.cpp output not found in %s", outputDir)
		}
		cand = matches[0]
	}

	dest := filepath.Join(outputDir, videoID+".captions."+langTag+".vtt")
	if filepath.Clean(cand) != filepath.Clean(dest) {
		if err := moveOrCopyFile(cand, dest); err != nil {
			return "", "", fmt.Errorf("whisper move: %w", err)
		}
	}
	return dest, langTag, nil
}

// whisperLibraryEnv keeps whisper.cpp on /usr/local/lib and out of Ollama's
// ggml, which aborts with GGML_ASSERT(device) when whisper-cli loads it.
func whisperLibraryEnv() []string {
	const whisperLib = "/usr/local/lib"
	env := os.Environ()
	out := make([]string, 0, len(env)+1)
	var rest []string
	for _, e := range env {
		if strings.HasPrefix(e, "LD_LIBRARY_PATH=") {
			for _, p := range strings.Split(strings.TrimPrefix(e, "LD_LIBRARY_PATH="), string(os.PathListSeparator)) {
				if p == "" || p == whisperLib || strings.Contains(filepath.ToSlash(p), "/lib/ollama") {
					continue
				}
				rest = append(rest, p)
			}
			continue
		}
		out = append(out, e)
	}
	ld := whisperLib
	if len(rest) > 0 {
		ld = whisperLib + string(os.PathListSeparator) + strings.Join(rest, string(os.PathListSeparator))
	}
	return append(out, "LD_LIBRARY_PATH="+ld)
}

func classifyWhisperFailure(err error, output string) error {
	if err == nil {
		return nil
	}
	msg := err.Error() + " " + output
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(msg, "libcuda.so") || strings.Contains(msg, "CUDA_ERROR"):
		return fmt.Errorf("%w: whisper.cpp needs a CUDA runtime (output=%s)", mlcore.ErrInfrastructure, strings.TrimSpace(output))
	case strings.Contains(low, "cannot open shared object file") ||
		strings.Contains(low, "error while loading shared libraries") ||
		strings.Contains(err.Error(), "exit status 127"):
		return fmt.Errorf("%w: whisper.cpp is not runnable (output=%s)", mlcore.ErrInfrastructure, strings.TrimSpace(output))
	default:
		return fmt.Errorf("whisper.cpp failed: %w (output=%s)", err, strings.TrimSpace(output))
	}
}

// probeWhisperCLI runs whisper-cli -h with a short timeout. Missing CUDA /
// broken dynamic linker fail here instead of after a minutes-long WAV extract.
func probeWhisperCLI(ctx context.Context, cfg whisperConfig) error {
	if _, err := exec.LookPath(cfg.Cmd); err != nil {
		return fmt.Errorf("%w: whisper-cli not found (%s)", mlcore.ErrWaitingModel, cfg.Cmd)
	}
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(pctx, cfg.Cmd, "-h")
	cmd.Env = whisperLibraryEnv()
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	if err == nil {
		return nil
	}
	classified := classifyWhisperFailure(err, buf.String())
	if errors.Is(classified, mlcore.ErrInfrastructure) || errors.Is(classified, mlcore.ErrWaitingModel) {
		return classified
	}
	// Non-zero -h is still a runnable binary (usage/help).
	return nil
}

func logWhisperStartupInfo() {
	cfg := loadWhisperConfig()
	slog.Info("whisper.cpp config",
		"cmd", cfg.Cmd,
		"model", cfg.Model,
		"model_dir", cfg.ModelDir,
		"language", cfg.Language,
		"task", cfg.Task,
		"device", cfg.Device,
	)
	if err := probeWhisperCLI(context.Background(), cfg); err != nil {
		slog.Warn("whisper-cli not runnable; transcribe will not be claimed until cooldown expires", "cmd", cfg.Cmd, "error", err)
	}
	path, err := whisperModelPath(cfg)
	if err != nil {
		slog.Warn("whisper model path", "error", err)
		return
	}
	if mlcore.ModelCheck(path) == mlcore.StatusWaitingModel {
		slog.Warn("whisper model missing; transcribe jobs will sit in waiting_model (will not download)", "path", path)
		return
	}
	slog.Info("whisper model present", "path", path)
}

func moveOrCopyFile(srcPath, destPath string) error {
	if err := os.Rename(srcPath, destPath); err == nil {
		return nil
	}
	in, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(destPath)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(destPath)
		return closeErr
	}
	_ = os.Remove(srcPath)
	return nil
}
