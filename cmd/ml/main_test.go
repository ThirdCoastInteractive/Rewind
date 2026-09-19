package main

import (
	"context"
	"reflect"
	"testing"
)

func TestMLWorkerKindGroups(t *testing.T) {
	textclsKinds := []string{"comment_classify", "speech_tone"}
	cuda := mlWorkerKindGroups("cuda")
	if len(cuda) != 2 {
		t.Fatalf("cuda groups=%d want 2", len(cuda))
	}
	want := []string{"visual_index", "transcribe", "context_windows", "refine_boundaries"}
	if !reflect.DeepEqual(cuda[0], want) {
		t.Fatalf("cuda kinds=%v want %v", cuda[0], want)
	}
	if !reflect.DeepEqual(cuda[1], textclsKinds) {
		t.Fatalf("cuda textcls kinds=%v want %v", cuda[1], textclsKinds)
	}
	if len(mlWorkerKindGroups("rocm")) != 2 {
		t.Fatal("rocm should share GPU worker plus textcls")
	}
	cpu := mlWorkerKindGroups("cpu")
	if len(cpu) != 4 {
		t.Fatalf("cpu groups=%d want 4", len(cpu))
	}
	if !reflect.DeepEqual(cpu[0], []string{"visual_index"}) ||
		!reflect.DeepEqual(cpu[1], []string{"transcribe"}) ||
		!reflect.DeepEqual(cpu[2], []string{"context_windows", "refine_boundaries"}) ||
		!reflect.DeepEqual(cpu[3], textclsKinds) {
		t.Fatalf("cpu split unexpected: %v", cpu)
	}
}

func TestMLJobSkipsOllamaForTextcls(t *testing.T) {
	if !mlJobSkipsOllama("comment_classify") || !mlJobSkipsOllama("speech_tone") {
		t.Fatal("textcls kinds must not take the Ollama lock")
	}
	if mlJobSkipsOllama("transcribe") {
		t.Fatal("transcribe uses the model lock")
	}
}

func TestLocalHTTPHost(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"http://127.0.0.1:3003", true},
		{"http://localhost:11434", true},
		{"https://127.0.0.1", true},
		{"127.0.0.1:11434", true},
		{"http://0.0.0.0:3003", true},
		{"http://vision:3003", false},
		{"http://host.docker.internal:3003", false},
		{"https://example.com", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := localHTTPHost(tc.host); got != tc.want {
			t.Fatalf("localHTTPHost(%q)=%v want %v", tc.host, got, tc.want)
		}
	}
}

func TestMaybeStartVisionSkipsRemote(t *testing.T) {
	t.Setenv("VISION_URL", "http://vision:3003")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	maybeStartVision(ctx)
}

func TestMaybeStartVisionSkipsMissingAppDir(t *testing.T) {
	t.Setenv("VISION_URL", "http://127.0.0.1:3003")
	t.Setenv("VISION_APP_DIR", t.TempDir()+"-missing")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	maybeStartVision(ctx)
}
