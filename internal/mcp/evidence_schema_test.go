package mcp

import (
	"context"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"testing"
)

func TestCompilationEvidenceOnNativeWire(t *testing.T) {
	ctx := context.Background()
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "fixture", Version: "1"}, nil)
	received := false
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "save_plan", Description: "fixture"}, func(_ context.Context, _ *mcpsdk.CallToolRequest, in *savePlanArgs) (*mcpsdk.CallToolResult, any, error) {
		evidence, ok := in.Segments[0].MatchEvidence.(map[string]any)
		if !ok || evidence["text"] != "Adam Sellers" {
			t.Errorf("evidence changed: %#v", in.Segments[0].MatchEvidence)
		}
		received = true
		return jsonResult(map[string]bool{"saved": true})
	})
	a, b := mcpsdk.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "fixture", Version: "1"}, nil)
	cs, err := client.Connect(ctx, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	result, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{Name: "save_plan", Arguments: map[string]any{"title": "fixture", "query": "Adam", "segments": []any{map[string]any{"video_id": "fixture", "start": 10, "end": 20, "match_evidence": map[string]any{"text": "Adam Sellers", "timestamp": 10, "language": "en"}}}}})
	if err != nil || result.IsError || !received {
		t.Fatalf("native JSON evidence rejected: %v %+v", err, result)
	}
}
