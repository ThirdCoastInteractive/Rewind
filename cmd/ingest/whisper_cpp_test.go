package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGgmlBinName(t *testing.T) {
	cases := map[string]string{
		"small":           "ggml-small.bin",
		"large-v3":        "ggml-large-v3.bin",
		"large-v3-turbo":  "ggml-large-v3-turbo.bin",
		"ggml-medium.bin": "ggml-medium.bin",
		"small.en":        "ggml-small.en.bin",
		"":                "ggml-small.bin",
	}
	for in, want := range cases {
		got, err := ggmlBinName(in)
		if err != nil {
			t.Fatalf("ggmlBinName(%q): %v", in, err)
		}
		if got != want {
			t.Errorf("ggmlBinName(%q)=%q want %q", in, got, want)
		}
	}
	if _, err := ggmlBinName("../evil"); err == nil {
		t.Fatal("expected invalid model name")
	}
}

func TestWhisperCLIArgs(t *testing.T) {
	cfg := whisperConfig{Language: "ja", Translate: true}
	args := whisperCLIArgs("/models/ggml-small.bin", "/tmp/a.wav", "/tmp/a", cfg)
	joined := strings.Join(args, " ")
	for _, want := range []string{"-m /models/ggml-small.bin", "-f /tmp/a.wav", "-ovtt", "-of /tmp/a", "-l ja", "-tr"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args %q missing %q", joined, want)
		}
	}
}

func TestWhisperCLIArgsAutoLangNoL(t *testing.T) {
	cfg := whisperConfig{Language: "auto"}
	args := whisperCLIArgs("m.bin", "a.wav", "out", cfg)
	for i, a := range args {
		if a == "-l" {
			t.Fatalf("auto language should not pass -l, got %v around %d", args, i)
		}
	}
}

func TestEnsureWhisperModelDownload(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("ggml-fake")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)

	cfg := whisperConfig{Model: "tiny", ModelDir: dir, ModelURL: srv.URL}
	path, err := ensureWhisperModel(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("got %q", got)
	}
	// Second call hits cache.
	path2, err := ensureWhisperModel(context.Background(), cfg)
	if err != nil || path2 != path {
		t.Fatalf("cache miss: %v %s", err, path2)
	}
	if filepath.Base(path) != "ggml-tiny.bin" {
		t.Fatalf("path %s", path)
	}
}
