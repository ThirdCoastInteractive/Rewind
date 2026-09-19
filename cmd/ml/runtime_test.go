package main

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestOllamaArchiveURL(t *testing.T) {
	got := ollamaArchiveURL("0.33.2", "linux", "amd64", "")
	want := "https://github.com/ollama/ollama/releases/download/v0.33.2/ollama-linux-amd64.tar.zst"
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	rocm := ollamaArchiveURL("0.33.2", "linux", "amd64", "-rocm")
	if rocm != "https://github.com/ollama/ollama/releases/download/v0.33.2/ollama-linux-amd64-rocm.tar.zst" {
		t.Fatalf("rocm url %s", rocm)
	}
}

func TestEnsureOllamaSkipsMatchingStamp(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "ollama"), []byte("ok"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OLLAMA_VERSION", "9.9.9")
	if err := writeStamp(dir, "ollama", "ollama=9.9.9"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("download should be skipped")
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RUNTIME_DIR", dir)
	if err := ensureOllama(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadExtractZst(t *testing.T) {
	var buf bytes.Buffer
	zw, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(zw)
	body := []byte("hi")
	if err := tw.WriteHeader(&tar.Header{Name: "bin/ollama", Mode: 0755, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(buf.Bytes())
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	if err := downloadExtractZst(context.Background(), srv.URL, dir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "bin", "ollama"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hi" {
		t.Fatalf("got %q", got)
	}
}

func TestStripOllamaExtrasCPU(t *testing.T) {
	dir := t.TempDir()
	lib := filepath.Join(dir, "lib", "ollama")
	for _, name := range []string{"cuda_v12", "mlx_cuda_v13", "vulkan"} {
		if err := os.MkdirAll(filepath.Join(lib, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("WHISPER_DEVICE", "cpu")
	t.Setenv("ML_RUNTIME", "")
	t.Setenv("VISION_DEVICE", "cpu")
	stripOllamaExtras(lib)
	if _, err := os.Stat(filepath.Join(lib, "cuda_v12")); !os.IsNotExist(err) {
		t.Fatal("cpu runtime should drop cuda_v12")
	}
	if _, err := os.Stat(filepath.Join(lib, "mlx_cuda_v13")); !os.IsNotExist(err) {
		t.Fatal("mlx should always be dropped")
	}
}

func TestStripOllamaExtrasCUDA(t *testing.T) {
	dir := t.TempDir()
	lib := filepath.Join(dir, "lib", "ollama")
	for _, name := range []string{"cuda_v12", "mlx_cuda_v13", "vulkan"} {
		if err := os.MkdirAll(filepath.Join(lib, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("WHISPER_DEVICE", "cuda")
	t.Setenv("ML_RUNTIME", "runtime-cuda")
	stripOllamaExtras(lib)
	if _, err := os.Stat(filepath.Join(lib, "cuda_v12")); err != nil {
		t.Fatal("cuda runtime should keep cuda_v12")
	}
	if _, err := os.Stat(filepath.Join(lib, "mlx_cuda_v13")); !os.IsNotExist(err) {
		t.Fatal("mlx should always be dropped")
	}
}

func TestDownloadExtractZstSkipsMLX(t *testing.T) {
	t.Setenv("WHISPER_DEVICE", "cpu")
	t.Setenv("VISION_DEVICE", "cpu")
	t.Setenv("ML_RUNTIME", "")
	var buf bytes.Buffer
	zw, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(zw)
	for _, name := range []string{"bin/ollama", "lib/ollama/mlx_cuda_v13/blob"} {
		body := []byte("hi")
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(buf.Bytes())
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	if err := downloadExtractZst(context.Background(), srv.URL, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "bin", "ollama")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "lib", "ollama", "mlx_cuda_v13", "blob")); !os.IsNotExist(err) {
		t.Fatal("mlx blob should not be extracted")
	}
}

func TestSkipOllamaExtractCUDA12(t *testing.T) {
	t.Setenv("WHISPER_DEVICE", "cuda")
	t.Setenv("ML_RUNTIME", "runtime-cuda")
	t.Setenv("OLLAMA_LLM_LIBRARY", "cuda_v12")
	if skipOllamaExtract("lib/ollama/cuda_v12/foo") {
		t.Fatal("should keep cuda_v12")
	}
	if !skipOllamaExtract("lib/ollama/cuda_v13/foo") {
		t.Fatal("should skip cuda_v13 when library is v12")
	}
	if !skipOllamaExtract("lib/ollama/mlx_cuda_v13/foo") {
		t.Fatal("should skip mlx")
	}
}

func TestWriteTarFileRejectsDotDot(t *testing.T) {
	dir := t.TempDir()
	hdr := &tar.Header{Name: "../evil", Typeflag: tar.TypeReg, Size: 2, Mode: 0644}
	if err := writeTarFile(dir, hdr, bytes.NewReader([]byte("no"))); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "evil")); err == nil {
		t.Fatal("wrote outside dest")
	}
}

func TestEnsureVisionSkipsMatchingStamp(t *testing.T) {
	dir := t.TempDir()
	lock := filepath.Join(dir, "requirements.lock")
	if err := os.WriteFile(lock, []byte("fastapi==0.115.12\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VISION_LOCK", lock)
	t.Setenv("VISION_DEVICE", "cpu")
	venv := filepath.Join(dir, "vision", "bin")
	if err := os.MkdirAll(venv, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(venv, "python"), []byte("ok"), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(lock)
	if err != nil {
		t.Fatal(err)
	}
	stamp := "vision=" + fmt.Sprintf("%x", sha256.Sum256(raw)) + " " + onnxruntimeCPU
	if err := writeStamp(dir, "vision", stamp); err != nil {
		t.Fatal(err)
	}
	if err := ensureVision(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
}
