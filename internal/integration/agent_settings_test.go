//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/application"
	"thirdcoast.systems/rewind/internal/config"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/runtimecfg"
)

func TestLiveSettingsAndRuntimeIsolation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool, err := application.OpenDBPoolWithRetry(ctx, config.Config{DatabaseDSN: "postgres://rewind_test:disposable-test-only@127.0.0.1:15439/rewind_test?sslmode=disable", DatabaseRetries: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); pool.Close() }()
	dbc := &db.DatabaseConnection{Pool: pool}
	if err = dbc.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	id := func() pgtype.UUID { return pgtype.UUID{Bytes: uuid.New(), Valid: true} }
	admin, user := id(), id()
	for _, account := range []pgtype.UUID{admin, user} {
		if _, err = pool.Exec(ctx, "INSERT INTO users(id,user_name,email,password,enabled) VALUES($1,$2,$2,'fixture',true)", account, account.String()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.Exec(ctx, "UPDATE users SET role='admin' WHERE id=$1", admin); err != nil {
		t.Fatal(err)
	}
	if err = runtimecfg.Save(ctx, dbc, user, runtimecfg.Snapshot{"agent.max_calls": 5}); err == nil {
		t.Fatal("non-admin changed settings")
	}
	if err = runtimecfg.Save(ctx, dbc, admin, runtimecfg.Snapshot{"agent.max_calls": 5, "agent.temperature": 0.3}); err != nil {
		t.Fatal(err)
	}
	if err = runtimecfg.Save(ctx, dbc, admin, runtimecfg.Snapshot{"agent.max_calls": 9, "agent.temperature": -1}); err == nil {
		t.Fatal("invalid patch accepted")
	}
	effective, err := runtimecfg.Read(ctx, dbc)
	if err != nil || effective["agent.max_calls"] != float64(5) {
		t.Fatalf("non-atomic patch: %v %v", effective, err)
	}
	if err = runtimecfg.Start(ctx, dbc, "web"); err != nil {
		t.Fatal(err)
	}
	if err = runtimecfg.Save(ctx, dbc, admin, runtimecfg.Snapshot{"agent.max_calls": 6}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(12 * time.Second)
	for runtimecfg.Int(ctx, "agent.max_calls") != 6 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if runtimecfg.Int(ctx, "agent.max_calls") != 6 {
		t.Fatal("settings reconciliation failed")
	}
	frozen, err := runtimecfg.Job(ctx, dbc, "fixture", user)
	if err != nil {
		t.Fatal(err)
	}
	if err = runtimecfg.Save(ctx, dbc, admin, runtimecfg.Snapshot{"agent.max_calls": 7}); err != nil {
		t.Fatal(err)
	}
	retry, err := runtimecfg.Job(ctx, dbc, "fixture", user)
	if err != nil {
		t.Fatal(err)
	}
	if runtimecfg.Int(retry, "agent.max_calls") != runtimecfg.Int(frozen, "agent.max_calls") {
		t.Fatal("retry changed its settings snapshot")
	}
	q := dbc.Queries(ctx)
	if err = q.MergeInterfacePreferences(ctx, &db.MergeInterfacePreferencesParams{UserID: user, Preferences: []byte(`{"theme":"federal","color_mode":"light"}`)}); err != nil {
		t.Fatal(err)
	}
	if err = q.MergeInterfacePreferences(ctx, &db.MergeInterfacePreferencesParams{UserID: user, Preferences: []byte(`{"sounds_enabled":false}`)}); err != nil {
		t.Fatal(err)
	}
	prefsRaw, err := q.GetInterfacePreferences(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	var prefs map[string]any
	if json.Unmarshal(prefsRaw, &prefs) != nil || prefs["theme"] != "federal" || prefs["color_mode"] != "light" || prefs["sounds_enabled"] != false {
		t.Fatalf("preference merge lost fields: %s", prefsRaw)
	}
	conversation, err := q.CreateAgentConversation(ctx, &db.CreateAgentConversationParams{UserID: user, Title: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = q.GetAgentConversation(ctx, &db.GetAgentConversationParams{ID: conversation.ID, UserID: admin}); err == nil {
		t.Fatal("cross-owner conversation exposed")
	}
	snapshot, _ := json.Marshal(runtimecfg.Defaults())
	run, err := q.CreateAgentRun(ctx, &db.CreateAgentRunParams{ConversationID: conversation.ID, UserID: user, Messages: []byte(`[{"role":"user","content":"fixture"}]`), Settings: snapshot})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "UPDATE agent_runs SET runtime='codex',status='waiting_approval' WHERE id=$1", run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = q.CreateAgentRun(ctx, &db.CreateAgentRunParams{ConversationID: conversation.ID, UserID: user, Messages: []byte(`[]`), Settings: snapshot}); err == nil {
		t.Fatal("duplicate run while waiting for approval")
	}
	if _, err = pool.Exec(ctx, "UPDATE agent_runs SET status='queued' WHERE id=$1", run.ID); err != nil {
		t.Fatal(err)
	}
	local, err := q.ClaimAgentRun(ctx, &db.ClaimAgentRunParams{Runtime: "local", LeaseOwner: "fixture"})
	if err == nil && local.ID == run.ID {
		t.Fatal("local worker claimed external runtime")
	}
	external, err := q.ClaimAgentRun(ctx, &db.ClaimAgentRunParams{Runtime: "codex", LeaseOwner: "external-fixture"})
	if err != nil || external.ID != run.ID {
		t.Fatalf("external claim: %v", err)
	}
	if _, err = q.StartAgentToolCall(ctx, &db.StartAgentToolCallParams{RunID: run.ID, LeaseOwner: "stale-owner", CallIndex: 0, Name: "enqueue_download", Arguments: []byte(`{}`)}); err == nil {
		t.Fatal("stale worker journaled a mutation")
	}
	call, err := q.StartAgentToolCall(ctx, &db.StartAgentToolCallParams{RunID: run.ID, LeaseOwner: "external-fixture", CallIndex: 0, Name: "fixture", Arguments: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if n, err := q.FinishAgentToolCall(ctx, &db.FinishAgentToolCallParams{ID: call.ID, LeaseOwner: "stale-owner", Result: []byte(`{}`)}); err != nil || n != 0 {
		t.Fatal("stale worker overwrote journal")
	}
	if n, err := q.AddAgentEvent(ctx, &db.AddAgentEventParams{RunID: run.ID, LeaseOwner: "stale-owner", Kind: "text", Data: []byte(`{}`)}); err != nil || n != 0 {
		t.Fatal("stale worker published events")
	}
	if _, err = q.FinishAgentRun(ctx, &db.FinishAgentRunParams{ID: run.ID, LeaseOwner: "external-fixture", Status: "completed"}); err != nil {
		t.Fatal(err)
	}
}
