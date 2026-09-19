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
	"thirdcoast.systems/rewind/internal/transcription"
	"thirdcoast.systems/rewind/pkg/captions"
	"thirdcoast.systems/rewind/pkg/utils/language"
)

func TestAgentRecoveryAndSearchScope(t *testing.T) {
	ctx := context.Background()
	pool, err := application.OpenDBPoolWithRetry(ctx, config.Config{DatabaseDSN: "postgres://rewind_test:disposable-test-only@127.0.0.1:15439/rewind_test?sslmode=disable", DatabaseRetries: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	dbc := &db.DatabaseConnection{Pool: pool}
	if err := dbc.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	id := func() pgtype.UUID { return pgtype.UUID{Bytes: uuid.New(), Valid: true} }
	user, video, other, catalogVideo, matchingCatalog := id(), id(), id(), id(), id()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO users(id,user_name,email,password) VALUES($1,$2,$2,'fixture')", user, user.String())
	for _, v := range []pgtype.UUID{video, other, catalogVideo, matchingCatalog} {
		exec("INSERT INTO videos(id,src,archived_by,title,uploader,media,video_path,probe_data) VALUES($1,$2,$3,'Tesla fixture','Ben fixture','file','/fixture.mp4','{\"format\":{\"duration\":\"180.25\"}}')", v, v.String(), user)
		exec("UPDATE videos SET search=to_tsvector('simple',title) WHERE id=$1", v)
	}
	exec("UPDATE videos SET media='metadata',uploader='unrelated fixture' WHERE id=$1", catalogVideo)
	exec("UPDATE videos SET media='metadata' WHERE id=$1", matchingCatalog)
	ctx = withToken(ctx, &db.APIToken{UserID: user, Scopes: []string{"mcp:read", "mcp:write"}})
	decode := func(r *mcpsdk.CallToolResult, err error) map[string]any {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		if r.IsError {
			t.Fatalf("tool error: %+v", r)
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(r.Content[0].(*mcpsdk.TextContent).Text), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	r, _, err := getTranscript(dbc)(ctx, nil, &getTranscriptArgs{ID: video.String()})
	missing := decode(r, err)
	if missing["status"] != "transcript_unavailable" || missing["next_tool"] != "enqueue_transcribe" {
		t.Fatalf("dead end: %+v", missing)
	}
	client, err := LocalSession(ctx, dbc, user)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var jobID any
	for i := 0; i < 2; i++ {
		r, err := client.CallTool(ctx, &mcpsdk.CallToolParams{Name: "enqueue_transcribe", Arguments: map[string]any{"video_id": video.String(), "start": 100, "end": 130}})
		out := decode(r, err)
		if i == 1 && out["job_id"] != jobID {
			t.Fatal("duplicate job")
		}
		jobID = out["job_id"]
	}
	r, err = client.CallTool(ctx, &mcpsdk.CallToolParams{Name: "get_transcription_status", Arguments: map[string]any{"job_id": jobID}})
	decode(r, err)
	for _, v := range []pgtype.UUID{video, other} {
		q, tx, err := dbc.NewWithTX(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := transcription.StoreRange(ctx, q, v, "en", []captions.Cue{{Start: 1, End: 3, Text: "Bryan Callan"}, {Start: 8, End: 10, Text: "bought a Tesla"}}, 100, 130); err != nil {
			tx.Rollback(ctx)
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	r, _, err = getTranscript(dbc)(ctx, nil, &getTranscriptArgs{ID: video.String()})
	partial := decode(r, err)
	if partial["lang"] != "en" || partial["complete"] != false {
		t.Fatalf("unusable transcript language or coverage: %+v", partial)
	}
	r, _, err = searchTranscriptPage(dbc, true)(ctx, nil, &transcriptSearchV2Args{VideoID: video.String(), QueryGroups: [][]string{{"Callen", "Callan"}, {"Tesla"}}, ContextSeconds: 30})
	out := decode(r, err)
	passages := out["passages"].([]any)
	if len(passages) == 0 {
		t.Fatal("missed cross-cue topic match")
	}
	for _, p := range passages {
		entry := p.(map[string]any)
		if entry["video_id"] != video.String() || entry["transcript_complete"] != false {
			t.Fatalf("scope or coverage: %+v", entry)
		}
	}
	// Both terms somewhere in a video must not qualify as one discussion.
	exec("UPDATE video_transcripts SET cues='[{\"start\":1,\"end\":3,\"text\":\"Bryan Callan\"},{\"start\":290,\"end\":292,\"text\":\"bought a Tesla\"}]'::jsonb WHERE video_id=$1", other)
	r, _, err = searchTranscriptPage(dbc, true)(ctx, nil, &transcriptSearchV2Args{VideoID: other.String(), QueryGroups: [][]string{{"Callen", "Callan"}, {"Tesla"}}, ContextSeconds: 30})
	if distant := decode(r, err); len(distant["passages"].([]any)) != 0 {
		t.Fatalf("unrelated distant topics matched: %+v", distant)
	}
	r, _, err = searchLibrary(dbc)(ctx, nil, &searchArgs{Query: "Tesla", Uploader: "Ben fixture", Limit: 10})
	library := decode(r, err)
	rows, ok := library["catalog_candidates"].([]any)
	if !ok || len(rows) == 0 {
		t.Fatalf("missing catalog matches: %+v", library)
	}
	for _, row := range rows {
		if row.(map[string]any)["uploader"] != "Ben fixture" {
			t.Fatalf("uploader leaked: %+v", row)
		}
	}
	var en language.Tag
	_ = en.Scan("en")
	if err := dbc.Queries(ctx).UpsertVideoTranscript(ctx, &db.UpsertVideoTranscriptParams{VideoID: video, Lang: en, Format: "vtt", Text: "full transcript", Cues: []byte(`[{"start":0,"end":3,"text":"full transcript"}]`)}); err != nil {
		t.Fatal(err)
	}
	q, tx, err := dbc.NewWithTX(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := transcription.StoreRange(ctx, q, video, "en", []captions.Cue{{Start: 0, End: 1, Text: "must not overwrite"}}, 100, 110); err != nil {
		t.Fatal(err)
	}
	tr, err := q.GetVideoTranscript(ctx, video)
	if err != nil || tr.Text != "full transcript" || len(tr.Coverage) != 0 {
		t.Fatalf("complete transcript overwritten: %+v %v", tr, err)
	}
	v, err := q.GetVideoByID(ctx, video)
	if err != nil || v.DurationSeconds == nil || *v.DurationSeconds != 181 {
		t.Fatalf("duration not repaired: %+v %v", v, err)
	}
}
