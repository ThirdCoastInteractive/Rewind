package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"thirdcoast.systems/rewind/pkg/captions"
)

func logWhisperStartupInfo() {
	cfg := loadWhisperConfig()
	slog.Info("whisper.cpp config",
		"enabled", cfg.Enabled,
		"cmd", cfg.Cmd,
		"model", cfg.Model,
		"model_dir", cfg.ModelDir,
		"language", cfg.Language,
		"task", cfg.Task,
		"device", cfg.Device,
	)
	if !cfg.Enabled {
		return
	}
	if _, err := exec.LookPath(cfg.Cmd); err != nil {
		slog.Warn("whisper-cli not found on PATH", "cmd", cfg.Cmd, "error", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	if path, err := ensureWhisperModel(ctx, cfg); err != nil {
		slog.Warn("whisper model not ready (will retry on first caption job)", "error", err)
	} else {
		slog.Info("whisper model ready", "path", path)
	}
	if strings.EqualFold(cfg.Device, "cuda") || strings.EqualFold(cfg.Device, "rocm") {
		logGPUDevices(cfg.Device)
	}
}

func logGPUDevices(device string) {
	hasDevice := false
	for _, p := range []string{"/dev/nvidia0", "/dev/nvidiactl", "/dev/nvidia-uvm", "/dev/kfd", "/dev/dri"} {
		if _, err := os.Stat(p); err == nil {
			hasDevice = true
			break
		}
	}
	if !hasDevice {
		slog.Warn("gpu device nodes not found", "device", device)
	}
	if smiPath, err := exec.LookPath("nvidia-smi"); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, smiPath, "-L")
		output, err := cmd.CombinedOutput()
		if err != nil {
			slog.Warn("nvidia-smi failed", "error", err, "output", strings.TrimSpace(string(output)))
		} else {
			slog.Info("nvidia-smi", "output", strings.TrimSpace(string(output)))
		}
	}
}

func whisperEnabled() bool {
	v := strings.TrimSpace(os.Getenv("WHISPER_ENABLED"))
	if v == "" {
		return true
	}
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

func findCanonicalCaptionFilePath(dir string, videoID string) (string, string, bool) {
	if strings.TrimSpace(dir) == "" || strings.TrimSpace(videoID) == "" {
		return "", "", false
	}
	candidates := []struct {
		path string
		lang string
	}{
		{path: filepath.Join(dir, videoID+".captions.en.vtt"), lang: "en"},
		{path: filepath.Join(dir, videoID+".captions.und.vtt"), lang: "und"},
	}
	for _, c := range candidates {
		if _, err := os.Stat(c.path); err == nil {
			return c.path, c.lang, true
		}
	}
	matches, _ := filepath.Glob(filepath.Join(dir, videoID+".captions.*.vtt"))
	for _, p := range matches {
		base := strings.ToLower(filepath.Base(p))
		if strings.HasSuffix(base, ".src.vtt") {
			continue
		}
		return p, captions.LangFromFilename(p), true
	}
	return "", "", false
}

func generateCaptionsWithWhisper(ctx context.Context, videoPath string, videoID string, outputDir string) (string, string, error) {
	cfg := loadWhisperConfig()
	if !cfg.Enabled {
		return "", "", fmt.Errorf("whisper disabled")
	}
	videoPath = strings.TrimSpace(videoPath)
	videoID = strings.TrimSpace(videoID)
	outputDir = strings.TrimSpace(outputDir)
	if videoPath == "" || videoID == "" || outputDir == "" {
		return "", "", fmt.Errorf("whisper: missing inputs")
	}

	cmdPath, err := exec.LookPath(cfg.Cmd)
	if err != nil {
		return "", "", fmt.Errorf("whisper-cli not found (%s): %w", cfg.Cmd, err)
	}
	modelPath, err := ensureWhisperModel(ctx, cfg)
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
	if err := extractWav16k(ctxToUse, videoPath, wavPath); err != nil {
		return "", "", err
	}

	outPrefix := filepath.Join(outputDir, videoID+".whisper")
	args := whisperCLIArgs(modelPath, wavPath, outPrefix, cfg)
	var buf bytes.Buffer
	cmd := exec.CommandContext(ctxToUse, cmdPath, args...)
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return "", "", fmt.Errorf("whisper.cpp failed: %w (output=%s)", err, strings.TrimSpace(buf.String()))
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
