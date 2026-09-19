package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"thirdcoast.systems/rewind/internal/db"
)

func TestActorFromMCPToken(t *testing.T) {
	user := pgtype.UUID{Bytes: uuid.MustParse("11111111-1111-1111-1111-111111111111"), Valid: true}
	ctx := withToken(context.Background(), &db.APIToken{
		UserID: user,
		Name:   "grok",
		Scopes: []string{"mcp:read", "mcp:write"},
	})
	ctx = withSession(ctx, sessionInfo{ID: "sess-1", ClientName: "claude", ClientVersion: "1.2.3"})
	a := ActorFrom(ctx)
	if a.Kind != ActorAgent || a.ID != "agent:claude:grok" {
		t.Fatalf("actor=%+v", a)
	}
	if a.TokenName != "grok" || a.SessionID != "sess-1" || a.ClientVersion != "1.2.3" {
		t.Fatalf("actor=%+v", a)
	}
	if a.UserIDString != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("user_id=%q", a.UserIDString)
	}
	if a.String() != "agent:claude:grok" {
		t.Fatalf("display=%q", a.String())
	}
}

func TestWhoamiReturnsTokenAndSession(t *testing.T) {
	user := pgtype.UUID{Bytes: uuid.MustParse("22222222-2222-2222-2222-222222222222"), Valid: true}
	ctx := withToken(context.Background(), &db.APIToken{
		UserID: user,
		Name:   "ci-bot",
		Scopes: []string{"mcp:read"},
	})
	ctx = withSession(ctx, sessionInfo{ID: "abc-session", ClientName: "cursor", ClientVersion: "0.9"})
	res, _, err := whoami(ctx, nil, &struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	text, ok := res.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("content=%T", res.Content[0])
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text.Text), &out); err != nil {
		t.Fatal(err)
	}
	if out["token_name"] != "ci-bot" || out["session_id"] != "abc-session" {
		t.Fatalf("whoami=%v", out)
	}
	if out["actor_kind"] != "agent" || out["actor_id"] != "agent:cursor:ci-bot" {
		t.Fatalf("whoami=%v", out)
	}
	if out["client_name"] != "cursor" || out["client_version"] != "0.9" {
		t.Fatalf("whoami=%v", out)
	}
	if out["user_id"] != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("whoami=%v", out)
	}
}

func TestWhoamiToolListed(t *testing.T) {
	ctx := context.Background()
	srv := newServer(nil)
	a, b := mcpsdk.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "9.9"}, nil)
	cs, err := client.Connect(ctx, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	listed, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range listed.Tools {
		if tool.Name == "whoami" {
			return
		}
	}
	t.Fatal("missing whoami")
}

func TestSessionIdentityMiddlewareCapturesClientInfo(t *testing.T) {
	user := pgtype.UUID{Bytes: uuid.MustParse("33333333-3333-3333-3333-333333333333"), Valid: true}
	srv := newServer(nil)
	srv.AddReceivingMiddleware(func(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
		return func(ctx context.Context, method string, req mcpsdk.Request) (mcpsdk.Result, error) {
			ctx = withToken(ctx, &db.APIToken{UserID: user, Name: "wire", Scopes: []string{"mcp:read"}})
			return next(ctx, method, req)
		}
	})
	a, b := mcpsdk.NewInMemoryTransports()
	ss, err := srv.Connect(context.Background(), a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "wire-client", Version: "2.0"}, nil)
	cs, err := client.Connect(context.Background(), b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	got, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "whoami"})
	if err != nil {
		t.Fatal(err)
	}
	text, ok := got.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("content=%T", got.Content[0])
	}
	if !strings.Contains(text.Text, `"token_name": "wire"`) {
		t.Fatalf("missing token_name: %s", text.Text)
	}
	if !strings.Contains(text.Text, `"client_name": "wire-client"`) {
		t.Fatalf("missing client_name: %s", text.Text)
	}
	if !strings.Contains(text.Text, `"client_version": "2.0"`) {
		t.Fatalf("missing client_version: %s", text.Text)
	}
	if !strings.Contains(text.Text, `"session_id":`) || strings.Contains(text.Text, `"session_id": ""`) {
		t.Fatalf("missing session_id: %s", text.Text)
	}
	if !strings.Contains(text.Text, `"actor_id": "agent:wire-client:wire"`) {
		t.Fatalf("missing actor_id: %s", text.Text)
	}
}
