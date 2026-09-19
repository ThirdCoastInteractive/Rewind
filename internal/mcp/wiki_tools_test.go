package mcp

import (
	"context"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"thirdcoast.systems/rewind/internal/db"
)

func TestWikiToolsRegistered(t *testing.T) {
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
	descs := map[string]string{}
	for _, tool := range listed.Tools {
		names[tool.Name] = true
		descs[tool.Name] = tool.Description
	}
	for _, name := range []string{"wiki_search", "wiki_get", "wiki_pages_for", "wiki_put", "wiki_history", "wiki_diff", "list_topic_windows"} {
		if !names[name] {
			t.Fatalf("missing %s", name)
		}
	}
	if !strings.Contains(descs["wiki_put"], "rewind://video/") || !strings.Contains(descs["wiki_put"], "rewind://clip/") {
		t.Fatalf("wiki_put should document inline media, got %q", descs["wiki_put"])
	}
}

func TestWikiPutRequiresWrite(t *testing.T) {
	fn := wikiPutMCP()
	if _, _, err := fn(context.Background(), nil, &wikiPutArgs{
		Tree: "creator", Slug: "ben-avery", Title: "Ben", Body: "x", Summary: "init",
	}); err == nil || !strings.Contains(err.Error(), "mcp:write") {
		t.Fatalf("expected write scope error, got %v", err)
	}
	ctx := withToken(context.Background(), &db.APIToken{Name: "fixture", Scopes: []string{"mcp:read"}})
	if _, _, err := fn(ctx, nil, &wikiPutArgs{
		Tree: "creator", Slug: "ben-avery", Title: "Ben", Body: "x", Summary: "init",
	}); err == nil || !strings.Contains(err.Error(), "mcp:write") {
		t.Fatalf("expected write scope error, got %v", err)
	}
}

func TestWikiPutRequiresSummary(t *testing.T) {
	fn := wikiPutMCP()
	ctx := withToken(context.Background(), &db.APIToken{Name: "fixture", Scopes: []string{"mcp:write"}})
	if _, _, err := fn(ctx, nil, &wikiPutArgs{
		Tree: "creator", Slug: "ben-avery", Title: "Ben", Body: "x", Summary: "  ",
	}); err == nil || !strings.Contains(err.Error(), "summary is required") {
		t.Fatalf("expected summary error, got %v", err)
	}
}

func TestClippingWorkflowMentionsWikiMemory(t *testing.T) {
	for _, s := range []string{"whoami", "wiki_pages_for", "wiki_put", "list_topic_windows", "Wiki is memory", "never treat a wiki timestamp as a cut", "rewind://video/", "rewind://clip/"} {
		if !strings.Contains(clippingWorkflow, s) {
			t.Fatalf("workflow missing %q", s)
		}
	}
}
