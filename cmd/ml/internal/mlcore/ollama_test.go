package mlcore

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDefaultContextModel(t *testing.T) {
	if DefaultContextModel != "qwen3.8:27b" {
		t.Fatalf("default=%s", DefaultContextModel)
	}
	if DefaultContextModel == "gpt-oss-20b" {
		t.Fatal("must not default gpt-oss-20b")
	}
}

func TestHasModelMissingWaiting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Fatalf("unexpected %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []any{}})
	}))
	t.Cleanup(srv.Close)
	o := &Ollama{BaseURL: srv.URL, HTTP: srv.Client()}
	_, err := o.HasModel(context.Background(), "qwen3.8:27b")
	if !errors.Is(err, ErrWaitingModel) {
		t.Fatalf("want waiting_model, got %v", err)
	}
}

func TestHasModelPresent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]string{{"name": "qwen3.8:27b", "digest": "sha256:abc"}},
		})
	}))
	t.Cleanup(srv.Close)
	o := &Ollama{BaseURL: srv.URL, HTTP: srv.Client()}
	d, err := o.HasModel(context.Background(), "qwen3.8:27b")
	if err != nil || d != "sha256:abc" {
		t.Fatalf("digest=%q err=%v", d, err)
	}
}

func TestGenerateWindowsInvalidThenValid(t *testing.T) {
	calls := 0
	valid := `{"windows":[{"title":"Scene","summary":"ok","topics":[],"entities":[],"cue_start":1,"cue_end":2}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Fatalf("path %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if len(body) == 0 {
			t.Fatal("empty body")
		}
		calls++
		content := "NOT JSON {"
		if calls > 1 {
			content = valid
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]string{"role": "assistant", "content": content},
		})
	}))
	t.Cleanup(srv.Close)
	o := &Ollama{BaseURL: srv.URL, HTTP: srv.Client(), Model: "qwen3.8:27b"}
	wins, err := o.GenerateWindows(context.Background(), "qwen3.8:27b", "[c0001 00:00:00.000-00:00:01.000] hi")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("want one repair retry (2 chats), got %d", calls)
	}
	if len(wins) != 1 || wins[0].Title != "Scene" {
		t.Fatalf("%+v", wins)
	}
}

func TestPickInstalledSkipsMissingPrimary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]string{{"name": "gemma4:12b", "digest": "d12"}},
		})
	}))
	t.Cleanup(srv.Close)
	o := &Ollama{BaseURL: srv.URL, HTTP: srv.Client()}
	name, digest, err := PickInstalledModel(context.Background(), o, "qwen3.8:27b", []string{"gemma4:26b", "gemma4:12b"})
	if err != nil || name != "gemma4:12b" || digest != "d12" {
		t.Fatalf("name=%s digest=%s err=%v", name, digest, err)
	}
}

func TestOllamaUnreachableWaiting(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	o := &Ollama{BaseURL: "http://127.0.0.1:1", HTTP: &http.Client{Timeout: 2 * time.Second}}
	_, err := o.HasModel(ctx, "qwen3.8:27b")
	if !errors.Is(err, ErrWaitingModel) {
		t.Fatalf("want waiting_model, got %v", err)
	}
	_, err = o.ChatJSON(ctx, "qwen3.8:27b", "sys", "user")
	if !errors.Is(err, ErrWaitingModel) {
		t.Fatalf("chat unreachable want waiting_model, got %v", err)
	}
}

func TestCanaryPreservesWaitingAndContentErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "model \"qwen3.8:27b\" not found, try pulling it"})
	}))
	t.Cleanup(srv.Close)
	o := &Ollama{BaseURL: srv.URL, HTTP: srv.Client()}
	err := o.Canary(context.Background(), "qwen3.8:27b", "digest")
	if !errors.Is(err, ErrWaitingModel) {
		t.Fatalf("want waiting_model, got %v", err)
	}
	if errors.Is(err, ErrInfrastructure) {
		t.Fatal("missing model must not trip infrastructure cooldown")
	}
}

func TestCanaryInvalidJSONIsContentError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]string{"role": "assistant", "content": "nope"},
		})
	}))
	t.Cleanup(srv.Close)
	o := &Ollama{BaseURL: srv.URL, HTTP: srv.Client()}
	err := o.Canary(context.Background(), "qwen3.8:27b", "digest")
	if err == nil || errors.Is(err, ErrInfrastructure) || errors.Is(err, ErrWaitingModel) {
		t.Fatalf("want content error, got %v", err)
	}
}

func TestChatJSONLlamaTimeoutIsInfrastructure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "timed out waiting for llama-server to start"})
	}))
	t.Cleanup(srv.Close)
	o := &Ollama{BaseURL: srv.URL, HTTP: srv.Client()}
	_, err := o.ChatJSON(context.Background(), "qwen3.8:27b", "sys", "user")
	if !errors.Is(err, ErrInfrastructure) {
		t.Fatalf("want infrastructure, got %v", err)
	}
}

func TestHasModelHTTP500IsInfrastructure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	o := &Ollama{BaseURL: srv.URL, HTTP: srv.Client()}
	_, err := o.HasModel(context.Background(), "qwen3.8:27b")
	if !errors.Is(err, ErrInfrastructure) {
		t.Fatalf("want infrastructure, got %v", err)
	}
}

func TestChatJSONLlamaKilledIsInfrastructure(t *testing.T) {
	const killed = `{"error":"llama-server process has terminated: signal: killed"}`
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{name: "http500", status: http.StatusInternalServerError, body: killed},
		{name: "json-error", status: http.StatusOK, body: killed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/chat" {
					t.Fatalf("path %s", r.URL.Path)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)
			o := &Ollama{BaseURL: srv.URL, HTTP: srv.Client()}
			_, err := o.ChatJSON(context.Background(), "qwen3.8:27b", "sys", "user")
			if !errors.Is(err, ErrInfrastructure) {
				t.Fatalf("want infrastructure, got %v", err)
			}
			if !strings.Contains(strings.ToLower(err.Error()), "killed") && !strings.Contains(strings.ToLower(err.Error()), "llama-server") {
				t.Fatalf("message: %v", err)
			}
			if _, ok := o.Unusable("qwen3.8:27b"); ok {
				t.Fatal("load-time llama-server death must not skip the model")
			}
		})
	}
}

func TestChatJSONLlamaKilledAfterResidentMarksUnusable(t *testing.T) {
	var chats int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chats++
		if chats == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"message": map[string]string{"role": "assistant", "content": `{"ok":true}`},
			})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"llama-server process has terminated: signal: killed"}`))
	}))
	t.Cleanup(srv.Close)
	o := &Ollama{BaseURL: srv.URL, HTTP: srv.Client()}
	if err := o.Canary(context.Background(), "qwen3.8:27b", "sha256:hot"); err != nil {
		t.Fatal(err)
	}
	if !o.Resident() {
		t.Fatal("canary should mark resident")
	}
	_, err := o.ChatJSON(context.Background(), "qwen3.8:27b", "sys", "user")
	if !errors.Is(err, ErrInfrastructure) {
		t.Fatalf("want infrastructure, got %v", err)
	}
	if _, ok := o.Unusable("qwen3.8:27b"); !ok {
		t.Fatal("killed resident 27b must be marked unusable")
	}
}

func TestPickInstalledSkipsDeadPrimary(t *testing.T) {
	var chats int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/chat":
			chats++
			if chats == 1 {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"message": map[string]string{"role": "assistant", "content": `{"ok":true}`},
				})
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"llama-server process has terminated: signal: killed"}`))
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]string{
					{"name": "qwen3.8:27b", "digest": "d27"},
					{"name": "gemma4:12b", "digest": "d12"},
				},
			})
		default:
			t.Fatalf("path %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	o := &Ollama{BaseURL: srv.URL, HTTP: srv.Client()}
	if err := o.Canary(context.Background(), "qwen3.8:27b", "d27"); err != nil {
		t.Fatal(err)
	}
	_, err := o.ChatJSON(context.Background(), "qwen3.8:27b", "sys", "user")
	if !errors.Is(err, ErrInfrastructure) {
		t.Fatalf("want infrastructure from killed 27b, got %v", err)
	}
	name, digest, err := PickInstalledModel(context.Background(), o, "qwen3.8:27b", []string{"gemma4:26b", "gemma4:12b"})
	if err != nil || name != "gemma4:12b" || digest != "d12" {
		t.Fatalf("want gemma4:12b after dead primary, name=%s digest=%s err=%v", name, digest, err)
	}
}

func TestPickInstalledDeadPrimaryNoChallengerWaiting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]string{{"name": "qwen3.8:27b", "digest": "d27"}},
		})
	}))
	t.Cleanup(srv.Close)
	o := &Ollama{BaseURL: srv.URL, HTTP: srv.Client()}
	o.MarkUnusable("qwen3.8:27b", "llama-server process has terminated: signal: killed")
	_, _, err := PickInstalledModel(context.Background(), o, "qwen3.8:27b", []string{"gemma4:26b", "gemma4:12b"})
	if !errors.Is(err, ErrWaitingModel) {
		t.Fatalf("want waiting_model, got %v", err)
	}
	if errors.Is(err, ErrInfrastructure) {
		t.Fatal("exhausted models must not keep infrastructure cooldown")
	}
	if !strings.Contains(err.Error(), "no smaller context model installed") {
		t.Fatalf("want clear error, got %v", err)
	}
}

func TestForJobSharesUnusable(t *testing.T) {
	o := &Ollama{Model: "qwen3.8:27b"}
	job := o.ForJob("qwen3.8:27b")
	job.MarkUnusable("qwen3.8:27b", "signal: killed")
	next := o.ForJob("qwen3.8:27b")
	if _, ok := next.Unusable("qwen3.8:27b"); !ok {
		t.Fatal("ForJob copies must share skip state")
	}
}

func TestForJobSharesCanaryDigest(t *testing.T) {
	var chats atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Fatalf("path %s", r.URL.Path)
		}
		chats.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]string{"role": "assistant", "content": `{"ok":true}`},
		})
	}))
	t.Cleanup(srv.Close)
	o := &Ollama{BaseURL: srv.URL, HTTP: srv.Client(), Model: "qwen3.8:27b"}
	job := o.ForJob("qwen3.8:27b")
	if err := job.Canary(context.Background(), "qwen3.8:27b", "sha256:shared"); err != nil {
		t.Fatal(err)
	}
	next := o.ForJob("qwen3.8:27b")
	if !next.AlreadyLoaded("sha256:shared") {
		t.Fatal("ForJob copies must share the verified digest")
	}
	if err := next.Canary(context.Background(), "qwen3.8:27b", "sha256:shared"); err != nil {
		t.Fatal(err)
	}
	if chats.Load() != 1 {
		t.Fatalf("sibling Canary must be a no-op, chats=%d", chats.Load())
	}
}

func TestShareLoadedOneCanaryManyJobs(t *testing.T) {
	var chats atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Fatalf("path %s", r.URL.Path)
		}
		chats.Add(1)
		time.Sleep(50 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]string{"role": "assistant", "content": `{"ok":true}`},
		})
	}))
	t.Cleanup(srv.Close)
	o := &Ollama{BaseURL: srv.URL, HTTP: srv.Client(), Model: "qwen3.8:27b"}
	var loads atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			job := o.ForJob("qwen3.8:27b")
			_, err := job.ShareLoaded(context.Background(), "qwen3.8:27b", "sha256:one", func() (func(), error) {
				loads.Add(1)
				return func() {}, nil
			})
			if err != nil {
				t.Errorf("ShareLoaded: %v", err)
			}
		}()
	}
	wg.Wait()
	if loads.Load() != 1 {
		t.Fatalf("load=%d want 1 (one admission for the shared model)", loads.Load())
	}
	if chats.Load() != 1 {
		t.Fatalf("chats=%d want 1 canary for 8 jobs", chats.Load())
	}
}
