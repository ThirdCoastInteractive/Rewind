package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func logWhisperStartupInfo() {
	enabled := whisperEnabled()

	cmdName := strings.TrimSpace(os.Getenv("WHISPER_CMD"))
	if cmdName == "" {
		cmdName = "whisper"
	}
	cmdPath, err := exec.LookPath(cmdName)
	if err != nil {
		slog.Warn("whisper command not found", "cmd", cmdName, "error", err)
		return
	}

	model := strings.TrimSpace(os.Getenv("WHISPER_MODEL"))
	if model == "" {
		model = "small"
	}
	device := strings.TrimSpace(os.Getenv("WHISPER_DEVICE"))
	if device == "" {
		device = "cpu"
	}
	lang := strings.TrimSpace(os.Getenv("WHISPER_LANGUAGE"))
	task := strings.TrimSpace(os.Getenv("WHISPER_TASK"))
	if task == "" {
		task = "transcribe"
	}

	slog.Info("whisper config", "enabled", enabled, "cmd", cmdPath, "model", model, "device", device, "language", lang, "task", task)

	if !enabled || !strings.EqualFold(device, "cuda") {
		return
	}

	hasDevice := false
	for _, p := range []string{"/dev/nvidia0", "/dev/nvidiactl", "/dev/nvidia-uvm"} {
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
	} else {
		slog.Warn("nvidia-smi not found")
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
	if len(matches) == 0 {
		return "", "", false
	}
	lang := "und"
	base := strings.ToLower(filepath.Base(matches[0]))
	parts := strings.Split(base, ".")
	if len(parts) >= 3 {
		cand := parts[len(parts)-2]
		if cand != "" && cand != "vtt" {
			lang = cand
		}
	}
	return matches[0], lang, true
}

func generateCaptionsWithWhisper(ctx context.Context, videoPath string, videoID string, outputDir string) (string, string, error) {
	if !whisperEnabled() {
		return "", "", fmt.Errorf("whisper disabled")
	}
	videoPath = strings.TrimSpace(videoPath)
	videoID = strings.TrimSpace(videoID)
	outputDir = strings.TrimSpace(outputDir)
	if videoPath == "" || videoID == "" || outputDir == "" {
		return "", "", fmt.Errorf("whisper: missing inputs")
	}

	cmdName := strings.TrimSpace(os.Getenv("WHISPER_CMD"))
	if cmdName == "" {
		cmdName = "whisper"
	}
	cmdPath, err := exec.LookPath(cmdName)
	if err != nil {
		return "", "", fmt.Errorf("whisper: command not found: %w", err)
	}

	model := strings.TrimSpace(os.Getenv("WHISPER_MODEL"))
	if model == "" {
		model = "small"
	}
	device := strings.TrimSpace(os.Getenv("WHISPER_DEVICE"))
	if device == "" {
		device = "cpu"
	}
	lang := strings.TrimSpace(os.Getenv("WHISPER_LANGUAGE"))
	langTag := "und"
	useLang := false
	if lang != "" && !strings.EqualFold(lang, "auto") {
		useLang = true
		langTag = lang
	}

	task := strings.TrimSpace(os.Getenv("WHISPER_TASK"))
	if task == "" {
		task = "transcribe"
	}

	args := []string{
		videoPath,
		"--model", model,
		"--output_format", "vtt",
		"--output_dir", outputDir,
		"--device", device,
		"--task", task,
	}
	if useLang {
		args = append(args, "--language", lang)
	}
	if extra := strings.TrimSpace(os.Getenv("WHISPER_ARGS")); extra != "" {
		args = append(args, strings.Fields(extra)...)
	}

	ctxToUse := ctx
	if timeout := strings.TrimSpace(os.Getenv("WHISPER_TIMEOUT_SECONDS")); timeout != "" {
		if n, err := strconv.Atoi(timeout); err == nil && n > 0 {
			var cancel context.CancelFunc
			ctxToUse, cancel = context.WithTimeout(ctx, time.Duration(n)*time.Second)
			defer cancel()
		}
	}

	var buf bytes.Buffer
	cmd := exec.CommandContext(ctxToUse, cmdPath, args...)
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return "", "", fmt.Errorf("whisper failed: %w (output=%s)", err, strings.TrimSpace(buf.String()))
	}

	base := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	cand := filepath.Join(outputDir, base+".vtt")
	if _, err := os.Stat(cand); err != nil {
		glob := filepath.Join(outputDir, base+"*.vtt")
		matches, _ := filepath.Glob(glob)
		if len(matches) == 0 {
			return "", "", fmt.Errorf("whisper output not found in %s", outputDir)
		}
		cand = matches[0]
	}

	dest := filepath.Join(outputDir, videoID+".captions."+langTag+".vtt")
	if _, err := os.Stat(dest); err == nil {
		return dest, langTag, nil
	}
	if filepath.Clean(cand) != filepath.Clean(dest) {
		if err := moveOrCopyFile(cand, dest); err != nil {
			return "", "", fmt.Errorf("whisper move: %w", err)
		}
	}

	return dest, langTag, nil
}
