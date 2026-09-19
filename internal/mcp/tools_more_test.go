package mcp

import (
	"context"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"thirdcoast.systems/rewind/internal/db"
)

func TestGetIndexStatusRequiresAuthAndID(t *testing.T) {
	fn := getIndexStatus(nil)
	if _, _, err := fn(context.Background(), nil, &indexStatusArgs{JobID: "00000000-0000-0000-0000-000000000001"}); err == nil || !strings.Contains(err.Error(), "authentication") {
		t.Fatalf("expected auth error, got %v", err)
	}
	ctx := withToken(context.Background(), &db.APIToken{Scopes: []string{"mcp:read"}})
	if _, _, err := fn(ctx, nil, &indexStatusArgs{JobID: "not-a-uuid"}); err == nil {
		t.Fatal("expected uuid error")
	}
}

func TestIndexURLToolListed(t *testing.T) {
	ctx := context.Background()
	srv := newServer(nil)
	a, b := mcpsdk.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "test"}, nil)
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
	for _, name := range []string{"index_url", "get_index_status"} {
		if !names[name] {
			t.Fatalf("missing %s", name)
		}
	}
}

func TestIndexURLRequiresWriteAndURL(t *testing.T) {
	fn := indexURL(nil)
	if _, _, err := fn(context.Background(), nil, &indexURLArgs{URL: "https://www.youtube.com/watch?v=ggLajT7aMMk"}); err == nil || !strings.Contains(err.Error(), "mcp:write") {
		t.Fatalf("expected write scope error, got %v", err)
	}
	ctx := withToken(context.Background(), &db.APIToken{Scopes: []string{"mcp:write"}})
	if _, _, err := fn(ctx, nil, &indexURLArgs{URL: "  "}); err == nil || !strings.Contains(err.Error(), "url is required") {
		t.Fatalf("expected url required, got %v", err)
	}
}
