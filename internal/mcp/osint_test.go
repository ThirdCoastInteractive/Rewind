package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"thirdcoast.systems/rewind/internal/db"
)

func TestOSINTToolsOnMCPWire(t *testing.T) {
	ctx := context.Background()
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "rewind", Version: "test"}, nil)
	registerOSINTTools(srv, nil)
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
		"get_osint_workflow", "search_commenters", "get_commenter", "list_commenter_comments",
		"list_osint_flags", "list_campaigns", "get_campaign", "get_video_comment_tone", "get_video_speech_tone",
		"watch_commenter", "unwatch_commenter", "dismiss_osint_flag", "assert_commenter_link",
		"enqueue_comment_classify", "enqueue_speech_tone", "index_x_replies",
		"get_channel_comment_engagement",
	} {
		if !names[name] {
			t.Fatalf("missing OSINT tool %s", name)
		}
	}
	guide, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{Name: "get_osint_workflow", Arguments: map[string]any{}})
	if err != nil || guide.IsError || len(guide.Content) == 0 {
		t.Fatal("guide unavailable", err)
	}
	text := ""
	if tc, ok := guide.Content[0].(*mcpsdk.TextContent); ok {
		text = tc.Text
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatal(err)
	}
	workflow, _ := payload["workflow"].(string)
	if !strings.Contains(workflow, "observe") {
		t.Fatal("workflow missing observe")
	}
	if strings.Contains(strings.ToLower(workflow), "ban") {
		t.Fatal("workflow must not treat ban as an action")
	}
}

func TestSearchCommentersEmptyQuery(t *testing.T) {
	fn := searchCommentersMCP(nil)
	res, _, err := fn(context.Background(), nil, &searchCommentersArgs{Query: ""})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatal("expected empty list result")
	}
	text := res.Content[0].(*mcpsdk.TextContent).Text
	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatal(err)
	}
	list, ok := payload["commenters"].([]any)
	if !ok || len(list) != 0 {
		t.Fatalf("expected empty commenters list, got %#v", payload["commenters"])
	}
}

func TestOSINTWriteToolsRequireWrite(t *testing.T) {
	id := "00000000-0000-0000-0000-000000000001"
	cases := []struct {
		name string
		call func(context.Context) error
	}{
		{"watch_commenter", func(ctx context.Context) error {
			_, _, err := watchCommenterMCP(nil)(ctx, nil, &watchCommenterArgs{CommenterID: id})
			return err
		}},
		{"unwatch_commenter", func(ctx context.Context) error {
			_, _, err := unwatchCommenterMCP(nil)(ctx, nil, &commenterIDArgs{CommenterID: id})
			return err
		}},
		{"dismiss_osint_flag", func(ctx context.Context) error {
			_, _, err := dismissOSINTFlagMCP(nil)(ctx, nil, &dismissOSINTFlagArgs{FlagID: id})
			return err
		}},
		{"assert_commenter_link", func(ctx context.Context) error {
			_, _, err := assertCommenterLinkMCP(nil)(ctx, nil, &assertCommenterLinkArgs{A: id, B: "00000000-0000-0000-0000-000000000002"})
			return err
		}},
		{"enqueue_comment_classify", func(ctx context.Context) error {
			_, _, err := enqueueCommentClassifyMCP(nil)(ctx, nil, &videoIDArgs{VideoID: id})
			return err
		}},
		{"enqueue_speech_tone", func(ctx context.Context) error {
			_, _, err := enqueueSpeechToneMCP(nil)(ctx, nil, &videoIDArgs{VideoID: id})
			return err
		}},
		{"index_x_replies", func(ctx context.Context) error {
			_, _, err := indexXRepliesMCP(nil)(ctx, nil, &indexXRepliesArgs{URL: "https://x.com/user/status/1"})
			return err
		}},
	}
	for _, tc := range cases {
		if err := tc.call(context.Background()); err == nil || !strings.Contains(err.Error(), "mcp:write") {
			t.Fatalf("%s: expected mcp:write without token, got %v", tc.name, err)
		}
		ctx := withToken(context.Background(), &db.APIToken{Name: "fixture", Scopes: []string{"mcp:read"}})
		if err := tc.call(ctx); err == nil || !strings.Contains(err.Error(), "mcp:write") {
			t.Fatalf("%s: expected mcp:write with read-only token, got %v", tc.name, err)
		}
	}
}
