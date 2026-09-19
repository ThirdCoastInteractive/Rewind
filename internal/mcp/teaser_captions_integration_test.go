//go:build integration

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"thirdcoast.systems/rewind/internal/application"
	"thirdcoast.systems/rewind/internal/config"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/stitch"
)

type teaserFixture struct {
	ctx            context.Context
	dbc            *db.DatabaseConnection
	pool           *pgxpool.Pool
	owner, other   pgtype.UUID
	project, video pgtype.UUID
	segmentID      string
}

func openTeaserFixture(t *testing.T) *teaserFixture {
	t.Helper()
	ctx := context.Background()
	p, err := application.OpenDBPoolWithRetry(ctx, config.Config{DatabaseDSN: "postgres://rewind_test:disposable-test-only@127.0.0.1:15439/rewind_test?sslmode=disable", DatabaseRetries: 1})
	if err != nil {
		t.Fatal(err)
	}
	f := &teaserFixture{ctx: ctx, pool: p, dbc: &db.DatabaseConnection{Pool: p}, owner: idUUID(), other: idUUID(), project: idUUID(), video: idUUID(), segmentID: "seg"}
	if err = f.dbc.Migrate(ctx); err != nil {
		p.Close()
		t.Fatal(err)
	}
	f.exec("INSERT INTO users(id,user_name,email,password) VALUES($1,$2,$2,'fixture'),($3,$4,$4,'fixture')", f.owner, f.owner.String(), f.other, f.other.String())
	f.exec("INSERT INTO videos(id,src,archived_by,title,duration_seconds) VALUES($1,$2,$3,'teaser fixture',10)", f.video, "https://example.invalid/"+f.video.String(), f.owner)
	f.exec(`INSERT INTO video_transcripts(video_id,lang,format,text,raw,cues) VALUES($1,'en','json','one two three four','', $2)`, f.video, []byte(`[{"start":1,"end":5,"text":"one two three four","words":[{"text":"one","start":1,"end":2},{"text":"two","start":2,"end":3},{"text":"three","start":3,"end":4},{"text":"four","start":4,"end":5}]}]`))
	return f
}

func (f *teaserFixture) exec(sql string, args ...any) {
	if _, err := f.pool.Exec(f.ctx, sql, args...); err != nil {
		panic(err)
	}
}

func (f *teaserFixture) close() { f.pool.Close() }

func (f *teaserFixture) createProject(t *testing.T, withCaption bool) {
	t.Helper()
	d := stitch.Document{Version: stitch.CurrentVersion, FPS: 30, Width: 1080, Height: 1920, Segments: []stitch.Segment{
		{ID: "seg", Type: "clip", VideoID: f.video.String(), StartUS: 0, DurationUS: 10_000_000},
		{ID: "seg2", Type: "clip", VideoID: f.video.String(), StartUS: 10_000_000, DurationUS: 5_000_000},
	}, TimingLinks: []stitch.Group{{ID: "link", Members: []string{"seg", "seg2"}}}}
	if withCaption {
		d.Captions = []stitch.Caption{{ID: "existing", SegmentID: "seg", Language: "en", Text: "existing", StartUS: 1_000_000, EndUS: 2_000_000, Alignment: "unaligned", SourceVideoID: f.video.String(), SourceStartUS: 1_000_000, SourceEndUS: 2_000_000}}
	}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	f.exec("INSERT INTO stitch_projects(id,created_by,title,document,editor_enabled) VALUES($1,$2,'fixture',$3,true)", f.project, f.owner, raw)
}

func idUUID() pgtype.UUID { return pgtype.UUID{Bytes: uuid.New(), Valid: true} }

func teaserWriteContext(f *teaserFixture, user pgtype.UUID) context.Context {
	return withToken(f.ctx, &db.APIToken{UserID: user, Scopes: []string{"mcp:read", "mcp:write"}})
}

func TestStyleTeaserCaptionsIntegration(t *testing.T) {
	f := openTeaserFixture(t)
	defer f.close()

	t.Run("import and style are atomic and preserve links and words", func(t *testing.T) {
		f.project = idUUID()
		f.createProject(t, false)
		ctx := teaserWriteContext(f, f.owner)
		_, _, err := styleTeaserCaptionsMCP(f.dbc)(ctx, nil, &styleTeaserCaptionsArgs{ProjectID: f.project.String(), Revision: ptrInt64(0), OperationKey: "atomic-import", SegmentID: f.segmentID, Language: "en", ImportCaptions: true, MaxCharsPerLine: 7, MaxLines: 1, MaxPhraseChars: 7})
		if err != nil {
			t.Fatal(err)
		}
		snap, err := stitch.NewStore(f.dbc).Get(f.ctx, f.owner, f.project)
		if err != nil {
			t.Fatal(err)
		}
		if len(snap.Document.Captions) != 3 || snap.Revision != 1 {
			t.Fatalf("unexpected committed captions: revision=%d captions=%d", snap.Revision, len(snap.Document.Captions))
		}
		for _, c := range snap.Document.Captions {
			if c.Alignment != "valid" || len(c.Words) == 0 || c.SourceVideoID != f.video.String() {
				t.Fatalf("source/alignment lost: %#v", c)
			}
			for _, w := range c.Words {
				if w.StartUS < c.StartUS || w.EndUS > c.EndUS {
					t.Fatalf("word escaped phrase: %#v", c)
				}
			}
		}
		if len(snap.Document.TimingLinks) != 1 || !containsTeaser(snap.Document.TimingLinks[0].Members, "seg2") {
			t.Fatalf("timing link lost: %#v", snap.Document.TimingLinks)
		}
		for _, c := range snap.Document.Captions {
			if !containsTeaser(snap.Document.TimingLinks[0].Members, c.ID) {
				t.Fatalf("caption missing from timing link: %s", c.ID)
			}
		}
	})

	t.Run("invalid style rolls back import", func(t *testing.T) {
		f.project = idUUID()
		f.createProject(t, false)
		ctx := teaserWriteContext(f, f.owner)
		if _, _, err := styleTeaserCaptionsMCP(f.dbc)(ctx, nil, &styleTeaserCaptionsArgs{ProjectID: f.project.String(), Revision: ptrInt64(0), OperationKey: "bad-style", SegmentID: f.segmentID, ImportCaptions: true, Style: "no-such-style"}); err == nil {
			t.Fatal("invalid style accepted")
		}
		snap, err := stitch.NewStore(f.dbc).Get(f.ctx, f.owner, f.project)
		if err != nil || snap.Revision != 0 || len(snap.Document.Captions) != 0 {
			t.Fatalf("invalid style mutated import: %#v %v", snap, err)
		}
	})

	t.Run("stale revision and foreign owner are rejected", func(t *testing.T) {
		f.project = idUUID()
		f.createProject(t, true)
		ctx := teaserWriteContext(f, f.owner)
		if _, _, err := styleTeaserCaptionsMCP(f.dbc)(ctx, nil, &styleTeaserCaptionsArgs{ProjectID: f.project.String(), Revision: ptrInt64(1), OperationKey: "stale", Style: "bold"}); err == nil || !errors.Is(err, stitch.ErrConflict) {
			t.Fatalf("stale revision error=%v", err)
		}
		foreign := teaserWriteContext(f, f.other)
		if _, _, err := styleTeaserCaptionsMCP(f.dbc)(foreign, nil, &styleTeaserCaptionsArgs{ProjectID: f.project.String(), Revision: ptrInt64(0), OperationKey: "foreign", Style: "bold"}); err == nil {
			t.Fatal("foreign owner wrote project")
		}
	})

	t.Run("replay is stable and changed payload conflicts", func(t *testing.T) {
		f.project = idUUID()
		f.createProject(t, true)
		ctx := teaserWriteContext(f, f.owner)
		args := &styleTeaserCaptionsArgs{ProjectID: f.project.String(), Revision: ptrInt64(0), OperationKey: "replay", Style: "basic"}
		first, _, err := styleTeaserCaptionsMCP(f.dbc)(ctx, nil, args)
		if err != nil {
			t.Fatal(err)
		}
		var firstJSON map[string]any
		if err = json.Unmarshal([]byte(first.Content[0].(*mcpsdk.TextContent).Text), &firstJSON); err != nil {
			t.Fatal(err)
		}
		store := stitch.NewStore(f.dbc)
		if _, err = store.Commit(f.ctx, f.owner, f.project, 1, "human-edit", stitch.Actor{Kind: "user", ID: f.owner.String()}, "human edit", []stitch.Operation{{Type: "set_title", Title: "human"}}); err != nil {
			t.Fatal(err)
		}
		// Simulate the layout tool changing the current canvas between the
		// original request and a lost-response retry. Canvas dimensions are
		// resolved inside the locked commit and are absent from the request hash.
		if _, err = f.pool.Exec(f.ctx, "UPDATE stitch_projects SET document=jsonb_set(document,'{width}','720'::jsonb) WHERE id=$1", f.project); err != nil {
			t.Fatal(err)
		}
		replayed, _, err := styleTeaserCaptionsMCP(f.dbc)(ctx, nil, args)
		if err != nil {
			t.Fatal(err)
		}
		var replayJSON map[string]any
		_ = json.Unmarshal([]byte(replayed.Content[0].(*mcpsdk.TextContent).Text), &replayJSON)
		if firstJSON["revision"] != replayJSON["revision"] {
			t.Fatalf("replay result changed: first=%v replay=%v", firstJSON["revision"], replayJSON["revision"])
		}
		current, err := store.Get(f.ctx, f.owner, f.project)
		if err != nil || current.Document.Title != "human" || current.Document.Width != 720 || current.Revision != 2 {
			t.Fatalf("replay changed current document: %#v %v", current, err)
		}
		if _, _, err = styleTeaserCaptionsMCP(f.dbc)(ctx, nil, &styleTeaserCaptionsArgs{ProjectID: f.project.String(), Revision: ptrInt64(0), OperationKey: "replay", Style: "bold"}); !errors.Is(err, stitch.ErrIdempotency) {
			t.Fatalf("changed payload error=%v", err)
		}
	})

	t.Run("segment id does not import when import is false", func(t *testing.T) {
		f.project = idUUID()
		f.createProject(t, true)
		ctx := teaserWriteContext(f, f.owner)
		if _, _, err := styleTeaserCaptionsMCP(f.dbc)(ctx, nil, &styleTeaserCaptionsArgs{ProjectID: f.project.String(), Revision: ptrInt64(0), OperationKey: "no-import", SegmentID: f.segmentID, ImportCaptions: false, Style: "bold"}); err != nil {
			t.Fatal(err)
		}
		snap, err := stitch.NewStore(f.dbc).Get(f.ctx, f.owner, f.project)
		if err != nil || len(snap.Document.Captions) != 1 {
			t.Fatalf("segment_id triggered import: %#v %v", snap, err)
		}
	})
}

func ptrInt64(v int64) *int64 { return &v }

func containsTeaser(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
