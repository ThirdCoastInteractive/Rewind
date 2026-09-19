package mcp

import (
	"context"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMLQueueToolsOnMCPWire(t *testing.T) {
	ctx := context.Background()
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "rewind", Version: "test"}, nil)
	registerMLTools(srv, nil)
	a, b := mcpsdk.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "generic-agent", Version: "test"}, nil)
	cs, err := client.Connect(ctx, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	listed, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range listed.Tools {
		names[tool.Name] = true
	}
	for _, name := range []string{
		"enqueue_context_windows", "enqueue_transcribe", "list_ml_jobs",
		"set_ml_job_priority", "cancel_ml_job", "clear_ml_queue", "retry_ml_job",
	} {
		if !names[name] {
			t.Fatalf("missing ML queue tool %s", name)
		}
	}
}
