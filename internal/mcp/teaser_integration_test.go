//go:build integration

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"thirdcoast.systems/rewind/internal/application"
	"thirdcoast.systems/rewind/internal/config"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/stitch"
)

func TestCreateTeaserRetryOwnershipAndSourceBounds(t *testing.T) {
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
	owner, other, video := id(), id(), id()
	for _, user := range []pgtype.UUID{owner, other} {
		if _, err := pool.Exec(ctx, "INSERT INTO users(id,user_name,email,password,enabled) VALUES($1,$2,$2,'fixture',true)", user, user.String()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, "INSERT INTO videos(id,src,archived_by,title,uploader,media,video_path,duration_seconds) VALUES($1,$2,$3,'teaser fixture','fixture','video','/fixture.mp4',40)", video, video.String(), owner); err != nil {
		t.Fatal(err)
	}
	auth := withToken(ctx, &db.APIToken{UserID: owner, Scopes: []string{"mcp:read", "mcp:write"}})
	makeArgs := func(key string) *createTeaserArgs {
		return &createTeaserArgs{Title: "Fixture teaser", OperationKey: key, Segments: []teaserRange{{VideoID: video.String(), Start: 10, End: 20}}}
	}
	first, _, err := createTeaser(dbc)(auth, nil, makeArgs("retry-key"))
	if err != nil {
		t.Fatal(err)
	}
	var firstOut struct {
		ProjectID string `json:"project_id"`
		Revision  int64  `json:"revision"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
	}
	decodeTeaserResult(t, first, &firstOut)
	if firstOut.Width != 1080 || firstOut.Height != 1920 || firstOut.Revision != 1 {
		t.Fatalf("unexpected defaults: %+v", firstOut)
	}
	layoutResult, _, err := setTeaserLayout(dbc)(auth, nil, &setTeaserLayoutArgs{ProjectID: firstOut.ProjectID, ExpectedRevision: &firstOut.Revision, OperationKey: "layout-key", TargetID: "teaser-segment-1", Layout: stitch.TeaserLayout{Mode: stitch.TeaserLayoutSingleSpeaker, Crops: []stitch.TeaserCrop{{X: 0, Y: 0, Width: 1, Height: 1}}}})
	if err != nil {
		t.Fatal("typed teaser layout failed:", err)
	}
	var layoutOut struct {
		Revision int64 `json:"revision"`
	}
	decodeTeaserResult(t, layoutResult, &layoutOut)
	if layoutOut.Revision != 2 {
		t.Fatalf("layout did not create revision 2: %+v", layoutOut)
	}
	if _, _, err := setTeaserLayout(dbc)(auth, nil, &setTeaserLayoutArgs{ProjectID: firstOut.ProjectID, ExpectedRevision: &firstOut.Revision, OperationKey: "stale-layout", TargetID: "teaser-segment-1", Layout: stitch.TeaserLayout{Mode: stitch.TeaserLayoutPreserveScene}}); !errors.Is(err, stitch.ErrConflict) {
		t.Fatalf("stale layout error=%v, want conflict", err)
	}
	retry, _, err := createTeaser(dbc)(auth, nil, makeArgs("retry-key"))
	if err != nil {
		t.Fatal("same request should be idempotent:", err)
	}
	var retryOut struct {
		ProjectID string `json:"project_id"`
	}
	decodeTeaserResult(t, retry, &retryOut)
	if retryOut.ProjectID != firstOut.ProjectID {
		t.Fatalf("retry created another project: %s != %s", retryOut.ProjectID, firstOut.ProjectID)
	}
	var retryDoc struct {
		Revision int64 `json:"revision"`
	}
	decodeTeaserResult(t, retry, &retryDoc)
	if retryDoc.Revision != 2 {
		t.Fatalf("retry regressed later layout revision: %+v", retryDoc)
	}
	selectedLayout := makeArgs("selected-layout")
	selectedLayout.Layout = &stitch.TeaserLayout{Mode: stitch.TeaserLayoutSingleSpeaker, Crops: []stitch.TeaserCrop{{X: 0, Y: 0, Width: 1, Height: 1}}}
	selected, _, err := createTeaser(dbc)(auth, nil, selectedLayout)
	if err != nil {
		t.Fatal("selected create layout failed:", err)
	}
	var selectedOut struct {
		Document stitch.Document `json:"document"`
	}
	decodeTeaserResult(t, selected, &selectedOut)
	if len(selectedOut.Document.Segments) != 1 || selectedOut.Document.Segments[0].Layout == nil || selectedOut.Document.Segments[0].Layout.Mode != stitch.TeaserLayoutSingleSpeaker {
		t.Fatalf("create layout was not persisted: %+v", selectedOut.Document.Segments)
	}
	checked, _, err := checkTeaser(dbc)(auth, nil, &checkTeaserArgs{ProjectID: firstOut.ProjectID})
	if err != nil {
		t.Fatal("teaser check failed:", err)
	}
	var checkOut struct {
		Issues    []map[string]any `json:"issues"`
		NextCalls []map[string]any `json:"next_calls"`
	}
	decodeTeaserResult(t, checked, &checkOut)
	if len(checkOut.Issues) < 2 || len(checkOut.NextCalls) != 2 {
		t.Fatalf("check did not report media/caption issues and preview calls: %+v", checkOut)
	}
	changed := makeArgs("retry-key")
	changed.Title = "changed request"
	if _, _, err := createTeaser(dbc)(auth, nil, changed); !errors.Is(err, stitch.ErrIdempotency) {
		t.Fatalf("changed idempotency payload error=%v, want ErrIdempotency", err)
	}
	if _, _, err := createTeaser(dbc)(auth, nil, &createTeaserArgs{OperationKey: "missing-source", Segments: []teaserRange{{VideoID: uuid.NewString(), Start: 1, End: 2}}}); err == nil {
		t.Fatal("missing source accepted")
	}
	if _, _, err := createTeaser(dbc)(auth, nil, &createTeaserArgs{OperationKey: "out-of-bounds", Segments: []teaserRange{{VideoID: video.String(), Start: 35, End: 45}}}); err == nil {
		t.Fatal("out-of-bounds source accepted")
	}
	foreign := withToken(ctx, &db.APIToken{UserID: other, Scopes: []string{"mcp:read", "mcp:write"}})
	projectID, err := parseUUID(firstOut.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	// Projects are owner-only: another user's read is a missing project.
	if _, err := stitch.NewStore(dbc).Get(foreign, other, projectID); !errors.Is(err, stitch.ErrNotFound) {
		t.Fatalf("foreign project read error=%v, want ErrNotFound", err)
	}
	if _, err := stitch.NewStore(dbc).Commit(foreign, other, projectID, firstOut.Revision, "foreign-write", stitch.Actor{Kind: "user", ID: other.String()}, "foreign", []stitch.Operation{{Type: "set_title", Title: "foreign"}}); !errors.Is(err, stitch.ErrNotFound) {
		t.Fatalf("foreign project write error=%v, want ErrNotFound", err)
	}

	var wg sync.WaitGroup
	results := make(chan string, 2)
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, _, e := createTeaser(dbc)(auth, nil, makeArgs("concurrent-key"))
			if e != nil {
				errs <- e
				return
			}
			var out struct {
				ProjectID string `json:"project_id"`
			}
			decodeTeaserResult(t, r, &out)
			results <- out.ProjectID
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		t.Fatal("concurrent idempotent create failed:", e)
	}
	var ids []string
	for project := range results {
		ids = append(ids, project)
	}
	if len(ids) != 2 || ids[0] != ids[1] {
		t.Fatalf("concurrent retries diverged: %v", ids)
	}
}

func TestScopeOnlyTeasersPaginatesWithinOneEpisodeAndAcrossEpisodes(t *testing.T) {
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
	owner := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	creator := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	channel := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	video := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	secondVideo := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	if _, err := pool.Exec(ctx, "INSERT INTO users(id,user_name,email,password,enabled) VALUES($1,$2,$2,'fixture',true)", owner, owner.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO creators(id,name) VALUES($1,'scope creator')", creator); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO channels(id,platform,identity_key,uploader,creator_id) VALUES($1,'youtube',$2,'fixture',$3)", channel, channel.String(), creator); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO videos(id,src,archived_by,title,uploader,media,video_path,duration_seconds) VALUES($1,$2,$3,'scope fixture','fixture','video','/fixture.mp4',120)", video, video.String(), owner); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO videos(id,src,archived_by,title,uploader,media,video_path,duration_seconds) VALUES($1,$2,$3,'scope fixture two','fixture','video','/fixture.mp4',120)", secondVideo, secondVideo.String(), owner); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE videos SET channel_row_id=$1 WHERE id=ANY($2::uuid[])", channel, []pgtype.UUID{video, secondVideo}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO video_transcripts(video_id,lang,format,text,raw,cues) VALUES($1,'en','vtt','one two three four five six seven eight nine','','[{"start":1,"end":3,"text":"one"},{"start":10,"end":12,"text":"two"},{"start":20,"end":22,"text":"three"},{"start":30,"end":32,"text":"four"},{"start":40,"end":42,"text":"five"},{"start":50,"end":52,"text":"six"},{"start":60,"end":62,"text":"seven"},{"start":70,"end":72,"text":"eight"},{"start":80,"end":82,"text":"nine"}]')`, video); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO video_transcripts(video_id,lang,format,text,raw,cues) VALUES($1,'en','vtt','ten eleven twelve thirteen fourteen fifteen sixteen seventeen eighteen','','[{"start":1,"end":3,"text":"ten"},{"start":10,"end":12,"text":"eleven"},{"start":20,"end":22,"text":"twelve"},{"start":30,"end":32,"text":"thirteen"},{"start":40,"end":42,"text":"fourteen"},{"start":50,"end":52,"text":"fifteen"},{"start":60,"end":62,"text":"sixteen"},{"start":70,"end":72,"text":"seventeen"},{"start":80,"end":82,"text":"eighteen"}]')`, secondVideo); err != nil {
		t.Fatal(err)
	}
	auth := withToken(ctx, &db.APIToken{UserID: owner, Scopes: []string{"mcp:read", "mcp:write"}})
	var paged []string
	var offset int32
	var passage int
	for page := 0; page < 10; page++ {
		result, _, err := suggestTeasers(dbc)(auth, nil, &teaserSuggestArgs{CreatorID: creator.String(), Limit: 3, Offset: offset, PassageOffset: passage})
		if err != nil {
			t.Fatal(err)
		}
		var response struct {
			Candidates []struct {
				Timestamp float64 `json:"timestamp"`
			} `json:"candidates"`
			NextOffset  int32 `json:"next_offset"`
			NextPassage int   `json:"next_passage_offset"`
			More        bool  `json:"has_more_candidates"`
		}
		decodeTeaserResult(t, result, &response)
		for _, c := range response.Candidates {
			paged = append(paged, fmt.Sprintf("%.1f", c.Timestamp))
		}
		if !response.More {
			break
		}
		offset, passage = response.NextOffset, response.NextPassage
		if page == 9 {
			t.Fatal("scope-only cursor did not exhaust")
		}
	}
	if len(paged) != 6 {
		t.Fatalf("scope-only pagination lost candidates: %v", paged)
	}
	result, _, err := suggestTeasers(dbc)(auth, nil, &teaserSuggestArgs{CreatorID: creator.String(), Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	var all struct {
		Candidates []struct {
			Timestamp float64 `json:"timestamp"`
		} `json:"candidates"`
	}
	decodeTeaserResult(t, result, &all)
	if len(all.Candidates) != len(paged) {
		t.Fatalf("small and large pages differ: paged=%v all=%d", paged, len(all.Candidates))
	}
}

func decodeTeaserResult(t *testing.T, result *mcpsdk.CallToolResult, out any) {
	t.Helper()
	if result == nil || len(result.Content) == 0 {
		t.Fatal("empty teaser result")
	}
	if err := json.Unmarshal([]byte(result.Content[0].(*mcpsdk.TextContent).Text), out); err != nil {
		t.Fatal(err)
	}
}
