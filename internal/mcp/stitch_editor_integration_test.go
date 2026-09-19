//go:build integration

package mcp

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"testing"
	"thirdcoast.systems/rewind/internal/application"
	"thirdcoast.systems/rewind/internal/config"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/stitch"
)

func TestStitchEditorMCPWireParity(t *testing.T) {
	ctx := context.Background()
	pool, err := application.OpenDBPoolWithRetry(ctx, config.Config{DatabaseDSN: "postgres://rewind_test:disposable-test-only@127.0.0.1:15439/rewind_test?sslmode=disable", DatabaseRetries: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	dbc := &db.DatabaseConnection{Pool: pool}
	if err = dbc.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	u := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	p := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	if _, err = pool.Exec(ctx, "INSERT INTO users(id,user_name,email,password,enabled) VALUES($1,$2,$3,'fixture',true)", u, u.String(), u.String()+"@test"); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "INSERT INTO stitch_projects(id,created_by,title,segments,global_filters) VALUES($1,$2,'Untitled','[]','[]')", p, u); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), "DELETE FROM users WHERE id=$1", u)
	tok := &db.APIToken{UserID: u, Scopes: []string{"mcp:read", "mcp:write"}}
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "rewind", Version: "test"}, nil)
	srv.AddReceivingMiddleware(func(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
		return func(c context.Context, method string, req mcpsdk.Request) (mcpsdk.Result, error) {
			return next(withToken(c, tok), method, req)
		}
	})
	registerStitchEditorTools(srv, dbc)
	a, b := mcpsdk.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := client.Connect(ctx, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	call := func(name string, args map[string]any) map[string]any {
		t.Helper()
		r, e := cs.CallTool(ctx, &mcpsdk.CallToolParams{Name: name, Arguments: args})
		if e != nil {
			t.Fatal(e)
		}
		if r.IsError {
			if tc, ok := r.Content[0].(*mcpsdk.TextContent); ok {
				t.Fatalf("%s error: %s", name, tc.Text)
			}
			t.Fatalf("%s error", name)
		}
		var out map[string]any
		if e = json.Unmarshal([]byte(r.Content[0].(*mcpsdk.TextContent).Text), &out); e != nil {
			t.Fatal(e)
		}
		return out
	}
	inspect := call("stitch_inspect", map[string]any{"project_id": p.String()})
	if inspect["revision"] != float64(0) {
		t.Fatalf("inspect=%v", inspect)
	}
	if _, ok := inspect["resolved"]; !ok {
		t.Fatal("missing resolved")
	}
	apply := call("stitch_apply", map[string]any{"project_id": p.String(), "expected_revision": float64(0), "operation_key": "mcp-title", "summary": "title", "operations": []any{map[string]any{"type": "set_title", "title": "MCP"}}})
	if apply["revision"] != float64(1) {
		t.Fatalf("apply=%v", apply)
	}
	if apply["summary"] != "title" || apply["edit_id"] == nil || apply["changed_ids"] == nil {
		t.Fatalf("missing command attribution/result: %v", apply)
	}
	retry := call("stitch_apply", map[string]any{"project_id": p.String(), "expected_revision": float64(0), "operation_key": "mcp-title", "summary": "title", "operations": []any{map[string]any{"type": "set_title", "title": "MCP"}}})
	if retry["edit_id"] != apply["edit_id"] || retry["revision"] != apply["revision"] {
		t.Fatalf("retry changed committed edit: %v", retry)
	}
	stale := cs
	_ = stale
	r, e := cs.CallTool(ctx, &mcpsdk.CallToolParams{Name: "stitch_apply", Arguments: map[string]any{"project_id": p.String(), "expected_revision": float64(0), "operation_key": "mcp-stale", "summary": "stale", "operations": []any{map[string]any{"type": "set_title", "title": "bad"}}}})
	if e != nil || !r.IsError {
		t.Fatalf("stale accepted: %v %v", r, e)
	}
	encodedConflict, _ := json.Marshal(r.StructuredContent)
	var conflict map[string]any
	_ = json.Unmarshal(encodedConflict, &conflict)
	if conflict["current_revision"] != float64(1) {
		t.Fatalf("missing conflict revision: %s", encodedConflict)
	}
	state := call("stitch_inspect", map[string]any{"project_id": p.String()})
	if state["revision"] != float64(1) {
		t.Fatalf("state=%v", state)
	}
	canonical, err := stitch.NewStore(dbc).Get(ctx, u, p)
	if err != nil || canonical.Document.Title != "MCP" {
		t.Fatalf("MCP/store state mismatch: %+v %v", canonical, err)
	}
	undone := call("stitch_undo", map[string]any{"project_id": p.String(), "expected_revision": 1, "operation_key": "mcp-undo", "summary": "Undo title"})
	if undone["revision"] != float64(2) || undone["summary"] != "Undo title" {
		t.Fatalf("undo response: %v", undone)
	}
	tok.UserID = pgtype.UUID{Bytes: uuid.New(), Valid: true}
	foreign, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{Name: "stitch_inspect", Arguments: map[string]any{"project_id": p.String()}})
	if err == nil && !foreign.IsError {
		t.Fatal("foreign project inspect accepted")
	}
}

var _ = stitch.Document{}
