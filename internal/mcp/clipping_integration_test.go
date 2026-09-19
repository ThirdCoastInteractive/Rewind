//go:build integration

package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"thirdcoast.systems/rewind/internal/application"
	"thirdcoast.systems/rewind/internal/config"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/stitch"
)

func TestAgentClippingWorkflow(t *testing.T) {
	// Fixed disposable service only; never use the archive's DATABASE_DSN.
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
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	id := func() pgtype.UUID { return pgtype.UUID{Bytes: uuid.New(), Valid: true} }
	user, creator, channel, video, other, translated := id(), id(), id(), id(), id(), id()
	exec("INSERT INTO users(id,user_name,email,password) VALUES($1,$2,$2,'fixture')", user, user.String())
	exec("INSERT INTO creators(id,name) VALUES($1,'Jeremy fixture')", creator)
	exec("INSERT INTO channels(id,platform,identity_key,uploader,creator_id) VALUES($1,'youtube',$2,'Jeremy fixture',$3)", channel, channel.String(), creator)
	for _, v := range []pgtype.UUID{video, other, translated} {
		exec("INSERT INTO videos(id,src,archived_by,title,uploader,media,video_path,duration_seconds) VALUES($1,$2,$3,'fixture','Jeremy fixture','video','/fixture.mp4',180)", v, v.String(), user)
	}
	exec("UPDATE videos SET channel_row_id=$1 WHERE id=ANY($2::uuid[])", channel, []pgtype.UUID{video, translated})
	exec(`INSERT INTO video_transcripts(video_id,lang,format,text,raw,cues) VALUES($1,'en','vtt','Adam Sellers then Adam again','','[{"start":10,"end":12,"text":"Adam Sellers"},{"start":90,"end":92,"text":"Adam again"}]')`, video)
	exec(`INSERT INTO video_transcripts(video_id,lang,format,text,raw,cues) VALUES($1,'en','vtt','Adam outside requested creator','','[{"start":10,"end":12,"text":"Adam outside requested creator"}]')`, other)
	exec(`INSERT INTO video_transcripts(video_id,lang,format,text,raw,cues) VALUES($1,'en','vtt','unrelated English','','[{"start":10,"end":12,"text":"unrelated English"}]'),($1,'fr','vtt','hot tubs','','[{"start":30,"end":32,"text":"hot tubs"}]')`, translated)
	exec("UPDATE video_transcripts SET search=to_tsvector('simple',text) WHERE video_id=ANY($1::uuid[])", []pgtype.UUID{video, other, translated})
	ctx = withToken(ctx, &db.APIToken{UserID: user, Scopes: []string{"mcp:read", "mcp:write"}})
	decode := func(result *mcpsdk.CallToolResult, err error, out any) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal([]byte(result.Content[0].(*mcpsdk.TextContent).Text), out); err != nil {
			t.Fatal(err)
		}
	}
	windowResult, _, err := createContextWindowMCP(dbc)(ctx, nil, &createContextArgs{VideoID: video.String(), Start: 5, End: 20, Title: "Adam discussion"})
	var window struct{ ID pgtype.UUID }
	decode(windowResult, err, &window)
	args := &transcriptSearchV2Args{Queries: []string{"Adam", `"Adam Sellers"`, "hot tub"}, CreatorID: creator.String(), Limit: 1}
	selected := []planSegmentInput{}
	seenFrench := false
	for page := 0; page < 10; page++ {
		result, _, err := searchTranscriptPage(dbc, true)(ctx, nil, args)
		var response struct {
			Passages []struct {
				Segment  planSegmentInput `json:"segment"`
				Language string           `json:"language"`
			}
			NextOffset  int32 `json:"next_offset"`
			NextPassage int   `json:"next_passage_offset"`
			More        bool  `json:"has_more_candidates"`
		}
		decode(result, err, &response)
		for _, p := range response.Passages {
			if p.Segment.VideoID == other.String() {
				t.Fatal("creator scope leaked")
			}
			selected = append(selected, p.Segment)
			seenFrench = seenFrench || p.Language == "fr"
		}
		if !response.More {
			break
		}
		args.Offset, args.PassageOffset = response.NextOffset, response.NextPassage
		if page == 9 {
			t.Fatal("continuation never exhausted")
		}
	}
	if len(selected) != 3 || !seenFrench {
		t.Fatalf("missing, duplicated, or wrong-language matches: %+v", selected)
	}
	foundWindow := false
	for _, s := range selected {
		if s.ContextWindowID == window.ID.String() {
			foundWindow = true
			if s.Start == 5 && s.End == 20 {
				t.Fatal("copied context window bounds", s)
			}
			if s.Start < 9.5 || s.End > 13 || s.End-s.Start > 5 {
				t.Fatal("expected spoken beat around 10-12, got", s)
			}
		}
	}
	if !foundWindow {
		t.Fatal("context window not used for clip candidate")
	}
	// The overlapping Adam aliases must yield one candidate per spoken passage.
	result, _, err := saveCompilationPlan(dbc)(ctx, nil, &savePlanArgs{Title: "Agent fixture", Query: "Adam and hot tubs", CreatorID: creator.String(), Segments: selected[:1]})
	var plan db.CompilationPlan
	decode(result, err, &plan)
	appendArgs := &updatePlanArgs{PlanID: plan.ID.String(), Revision: plan.Revision, Segments: selected[1:]}
	result, _, err = editCompilationPlan(dbc, true)(ctx, nil, appendArgs)
	decode(result, err, &plan)
	if _, _, err = editCompilationPlan(dbc, true)(ctx, nil, appendArgs); err == nil {
		t.Fatal("replayed append did not reject stale revision")
	}
	projectArgs := &stitchProjectArgs{PlanID: plan.ID.String(), Revision: plan.Revision}
	result, _, err = createStitchProjectMCP(dbc)(ctx, nil, projectArgs)
	var response struct {
		ProjectID    string `json:"project_id"`
		WebPath      string `json:"web_path"`
		RenderQueued bool   `json:"render_queued"`
	}
	decode(result, err, &response)
	if response.WebPath != "/stitch/"+response.ProjectID || response.RenderQueued {
		t.Fatal("not an editable project", response)
	}
	projectID, err := parseUUID(response.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := stitch.NewStore(dbc).Get(ctx, user, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Enabled || snap.Document.Version == 0 {
		t.Fatal("expected editor-enabled stitch document as live model")
	}
	legacyRaw, _, err := stitch.ToLegacy(snap.Document)
	if err != nil {
		t.Fatal(err)
	}
	var segments []struct {
		Type    string  `json:"type"`
		Text    string  `json:"text"`
		Font    string  `json:"font"`
		VideoID string  `json:"video_id"`
		Start   float64 `json:"start_ts"`
		End     float64 `json:"end_ts"`
	}
	if err = json.Unmarshal(legacyRaw, &segments); err != nil {
		t.Fatal(err)
	}
	if len(segments) == 0 || segments[0].Type != "title" || segments[0].Text != snap.Document.Title || segments[0].Font != "UnifrakturCook" {
		t.Fatal("missing gothic chapter title card", segments)
	}
	media := segments[1:]
	if len(media) != len(selected) {
		t.Fatal("segments lost")
	}
	for i, s := range media {
		if s.VideoID != selected[i].VideoID || s.Start != selected[i].Start || s.End != selected[i].End {
			t.Fatal("editorial bounds/order changed")
		}
	}
	exec("UPDATE stitch_projects SET title='human edit' WHERE id=$1", projectID)
	result, _, err = createStitchProjectMCP(dbc)(ctx, nil, projectArgs)
	var retried struct {
		ProjectID string `json:"project_id"`
	}
	decode(result, err, &retried)
	if retried.ProjectID != response.ProjectID {
		t.Fatal("retry duplicated project")
	}
	project, err := dbc.Queries(ctx).GetStitchProject(ctx, projectID)
	if err != nil || project.Title != "human edit" {
		t.Fatal("retry overwrote human edit", err)
	}
	snap, err = stitch.NewStore(dbc).Get(ctx, user, projectID)
	if err != nil || !snap.Enabled || snap.Document.Version == 0 {
		t.Fatal("retry cleared editor document", err)
	}
	jobs, err := dbc.Queries(ctx).ListStitchJobsByProject(ctx, projectID)
	if err != nil || len(jobs) != 0 {
		t.Fatal("project unexpectedly queued rendering", err)
	}
	foreign := withToken(context.Background(), &db.APIToken{UserID: id(), Scopes: []string{"mcp:write"}})
	if _, _, err = createStitchProjectMCP(dbc)(foreign, nil, projectArgs); err == nil {
		t.Fatal("foreign plan accepted")
	}
	if _, _, err = createStitchProjectMCP(dbc)(withToken(context.Background(), &db.APIToken{UserID: user, Scopes: []string{"mcp:read"}}), nil, projectArgs); err == nil {
		t.Fatal("read token created project")
	}
	updated, err := dbc.Queries(ctx).BumpCompilationPlanRevision(ctx, &db.BumpCompilationPlanRevisionParams{ID: plan.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = createStitchProjectMCP(dbc)(ctx, nil, projectArgs); err == nil {
		t.Fatal("stale revision accepted")
	}
	projectArgs.Revision = updated.Revision
	result, _, err = createStitchProjectMCP(dbc)(ctx, nil, projectArgs)
	decode(result, err, &retried)
	if retried.ProjectID == response.ProjectID {
		t.Fatal("new revision reused old project")
	}
}
