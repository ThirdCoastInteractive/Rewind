package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"thirdcoast.systems/rewind/cmd/ml/internal/mlcore"
)

func TestClassifyWhisperFailureCUDA(t *testing.T) {
	err := classifyWhisperFailure(errors.New("exit status 127"), "whisper-cli: error while loading shared libraries: libcuda.so.1: cannot open shared object file")
	if !errors.Is(err, mlcore.ErrInfrastructure) {
		t.Fatalf("want infrastructure, got %v", err)
	}
	if !strings.Contains(err.Error(), "CUDA") && !strings.Contains(err.Error(), "libcuda") {
		t.Fatalf("message: %v", err)
	}
}

func TestClassifyWhisperFailureExit127(t *testing.T) {
	err := classifyWhisperFailure(errors.New("exit status 127"), "error while loading shared libraries: libggml.so")
	if !errors.Is(err, mlcore.ErrInfrastructure) {
		t.Fatalf("want infrastructure, got %v", err)
	}
}

func TestClassifyWhisperFailureOther(t *testing.T) {
	err := classifyWhisperFailure(errors.New("exit status 1"), "out of memory")
	if errors.Is(err, mlcore.ErrInfrastructure) || errors.Is(err, mlcore.ErrWaitingModel) {
		t.Fatalf("ordinary failure classified as wait: %v", err)
	}
}

func TestWhisperLibraryEnvDropsOllamaGGML(t *testing.T) {
	t.Setenv("LD_LIBRARY_PATH", "/runtime/lib/ollama"+string(os.PathListSeparator)+"/usr/local/nvidia/lib64")
	env := whisperLibraryEnv()
	var ld string
	for _, e := range env {
		if strings.HasPrefix(e, "LD_LIBRARY_PATH=") {
			ld = strings.TrimPrefix(e, "LD_LIBRARY_PATH=")
		}
	}
	if !strings.HasPrefix(ld, "/usr/local/lib") {
		t.Fatalf("whisper lib not first: %q", ld)
	}
	if strings.Contains(ld, "ollama") {
		t.Fatalf("ollama ggml still on the path: %q", ld)
	}
	if !strings.Contains(ld, "/usr/local/nvidia/lib64") {
		t.Fatalf("nvidia path dropped: %q", ld)
	}
}

func TestGenerateCaptionsWhisperMissingCLI(t *testing.T) {
	t.Setenv("WHISPER_CMD", "whisper-cli-not-installed-xyz")
	_, _, err := generateCaptionsWithWhisperConfig(context.Background(), "v.mp4", "vid", t.TempDir(), loadWhisperConfig(context.Background()))
	if !errors.Is(err, mlcore.ErrWaitingModel) {
		t.Fatalf("want waiting_model, got %v", err)
	}
}

func TestFFmpegNoAudioOutput(t *testing.T) {
	msg := "Output file #0 does not contain any stream\nError opening output file /tmp/x.wav.\nError opening output files: Invalid argument"
	if !ffmpegNoAudioOutput(msg) {
		t.Fatal("expected no-audio detection")
	}
	err := fmt.Errorf("%w: %s", errNoAudio, msg)
	if !errors.Is(err, errNoAudio) {
		t.Fatalf("want errNoAudio, got %v", err)
	}
	if ffmpegNoAudioOutput("Conversion failed!\nError while decoding stream") {
		t.Fatal("ordinary ffmpeg failure classified as no-audio")
	}
}
