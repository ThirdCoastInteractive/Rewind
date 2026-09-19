package mcp

import (
	"context"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"testing"
)

func TestVisionToolCatalogHasNoFaceOperations(t *testing.T) {
	ctx := context.Background()
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "fixture", Version: "1"}, nil)
	registerVisionTools(srv, nil)
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
	result, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]bool{"index_visual_range": true, "visual_index_status": true, "search_visual_moments": true}
	for _, tool := range result.Tools {
		if !expected[tool.Name] {
			t.Errorf("unexpected vision tool: %s", tool.Name)
		}
		delete(expected, tool.Name)
	}
	if len(expected) > 0 {
		t.Errorf("missing visual tools: %v", expected)
	}
}
