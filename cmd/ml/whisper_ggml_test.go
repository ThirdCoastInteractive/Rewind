package main

import (
	"strings"
	"testing"
)

func TestGgmlBinName(t *testing.T) {
	cases := map[string]string{
		"":                "ggml-large-v3-turbo.bin",
		"large-v3-turbo":  "ggml-large-v3-turbo.bin",
		"small":           "ggml-small.bin",
		"large-v3":        "ggml-large-v3.bin",
		"ggml-medium.bin": "ggml-medium.bin",
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
