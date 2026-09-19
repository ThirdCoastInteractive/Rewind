package modelruntime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"thirdcoast.systems/rewind/internal/runtimecfg"
)

func TestConcurrentLeasesAndReleaseOnce(t *testing.T) {
	ctx := runtimecfg.WithSnapshot(context.Background(), runtimecfg.Snapshot{"ml.concurrent": 2})
	first, err := Acquire(ctx, "fixture-one")
	if err != nil {
		t.Fatal(err)
	}
	defer first()
	second, err := Acquire(ctx, "fixture-two")
	if err != nil {
		t.Fatal(err)
	}
	defer second()
	if _, err = Acquire(ctx, "fixture-three"); err == nil {
		t.Fatal("capacity exceeded")
	}
	first()
	first()
	if Active()["fixture-two"] != 1 {
		t.Fatal("another task's lease changed")
	}
	third, err := Acquire(ctx, "fixture-three")
	if err != nil {
		t.Fatal(err)
	}
	third()
}

func TestNativeToolStreamAndDisconnect(t *testing.T) {
	for _, done := range []bool{true, false} {
		t.Run(fmt.Sprint(done), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer "+Token() {
					t.Error("missing internal authentication")
				}
				fmt.Fprintln(w, `{"message":{"role":"assistant","tool_calls":[{"function":{"name":"search_library","arguments":{"query":"Adam"}}}]}}`)
				if done {
					fmt.Fprintln(w, `{"done":true,"message":{"content":""}}`)
				}
			}))
			defer server.Close()
			t.Setenv("ML_CONTROL_URL", server.URL)
			t.Setenv("ML_CONTROL_TOKEN", "fixture")
			result, err := Chat(context.Background(), ChatRequest{Model: "fixture"}, nil)
			if done && (err != nil || len(result.ToolCalls) != 1) {
				t.Fatalf("native tool lost: %v %v", result, err)
			}
			if !done && err == nil {
				t.Fatal("incomplete response accepted")
			}
		})
	}
}

func TestInferenceCancellation(t *testing.T) {
	cancelled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"message":{"content":"started"}}`)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(cancelled)
	}))
	defer server.Close()
	t.Setenv("ML_CONTROL_URL", server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	_, err := Chat(ctx, ChatRequest{Model: "fixture"}, func(Chunk) error { cancel(); return nil })
	if err == nil {
		t.Fatal("cancelled inference succeeded")
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("provider did not receive cancellation")
	}
}

func TestPendingLoadsReserveResidencyAndAliases(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ps" {
			t.Errorf("unexpected eviction: %s", r.URL.Path)
		}
		fmt.Fprintln(w, `{"models":[]}`)
	}))
	defer server.Close()
	t.Setenv("OLLAMA_HOST", server.URL)
	a, err := AcquireOllama(context.Background(), "pending:latest")
	if err != nil {
		t.Fatal(err)
	}
	defer a()
	b, err := AcquireOllama(context.Background(), "pending")
	if err != nil {
		t.Fatal(err)
	}
	defer b()
	if Active()["pending"] != 2 || reservations["pending"] != 2 {
		t.Fatal("alias bypassed shared reservation")
	}
	a()
	b()
	if len(reservations) != 0 {
		t.Fatal("reservation leaked")
	}
}

func TestLiveCapacityOverridesOldRunSnapshot(t *testing.T) {
	ctx := runtimecfg.WithSnapshot(context.Background(), runtimecfg.Snapshot{"ml.concurrent": 100})
	a, err := Acquire(ctx, "one")
	if err != nil {
		t.Fatal(err)
	}
	defer a()
	b, err := Acquire(ctx, "two")
	if err != nil {
		t.Fatal(err)
	}
	defer b()
	if release, err := Acquire(ctx, "three"); err == nil {
		release()
		t.Fatal("stale run configuration bypassed live admission limit")
	}
}
