//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/application"
	"thirdcoast.systems/rewind/internal/config"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/stitch"
)

func openStitchTest(t *testing.T) (*db.DatabaseConnection, pgtype.UUID, pgtype.UUID) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	name := "rewind_test"
	if requested := os.Getenv("STITCH_TEST_DATABASE"); requested != "" {
		if !regexp.MustCompile(`^rewind_test_[a-z0-9_]+$`).MatchString(requested) {
			t.Fatal("STITCH_TEST_DATABASE must name a disposable rewind_test_* database")
		}
		name = requested
	}
	p, err := application.OpenDBPoolWithRetry(ctx, config.Config{DatabaseDSN: "postgres://rewind_test:disposable-test-only@127.0.0.1:15439/" + name + "?sslmode=disable", DatabaseRetries: 1})
	if err != nil {
		t.Fatal(err)
	}
	d := &db.DatabaseConnection{Pool: p}
	if err = d.Migrate(ctx); err != nil {
		p.Close()
		t.Fatal(err)
	}
	u := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	project := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	if _, err = p.Exec(ctx, "INSERT INTO users(id,user_name,email,password,enabled) VALUES($1,$2,$3,'fixture',true)", u, u.String(), u.String()+"@test"); err != nil {
		p.Close()
		t.Fatal(err)
	}
	if _, err = p.Exec(ctx, "INSERT INTO stitch_projects(id,created_by,title,segments,global_filters) VALUES($1,$2,'Untitled','[]','[]')", project, u); err != nil {
		p.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = p.Exec(context.Background(), "DELETE FROM users WHERE id=$1", u); p.Close() })
	return d, u, project
}

func TestStitchLegacyClipHydration(t *testing.T) {
	d, u, p := openStitchTest(t)
	ctx := context.Background()
	clip := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	video := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	if _, err := d.Exec(ctx, `INSERT INTO videos(id,src,archived_by,title) VALUES($1,$2,$3,'source')`, video, "fixture-source-"+video.String(), u); err != nil {
		t.Fatal(err)
	}
	legacy, _ := json.Marshal([]map[string]any{{"id": "seg-1", "type": "clip", "clip_id": clip.String(), "title": "kept", "gain_db": -2.0}})
	if _, err := d.Exec(ctx, `INSERT INTO clips(id,video_id,start_ts,end_ts,duration,created_by,title) VALUES($1,$2,100,110,10,$3,'clip')`, clip, video, u); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(ctx, `UPDATE stitch_projects SET segments=$1 WHERE id=$2`, legacy, p); err != nil {
		t.Fatal(err)
	}
	s := stitch.NewStore(d)
	snap, err := s.Enable(ctx, u, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Document.Segments) != 1 || snap.Document.Segments[0].SourceInUS != 100000000 || snap.Document.Segments[0].DurationUS != 10000000 {
		t.Fatalf("hydration failed: %+v", snap.Document.Segments)
	}
	var frozen map[string]any
	if err := json.Unmarshal(snap.Document.Segments[0].Legacy, &frozen); err != nil || frozen["title"] != "kept" || frozen["gain_db"] != float64(-2) {
		t.Fatalf("legacy fields not frozen: %v %v", frozen, err)
	}
	if _, err := d.Exec(ctx, `UPDATE clips SET start_ts=200,end_ts=202,duration=2 WHERE id=$1`, clip); err != nil {
		t.Fatal(err)
	}
	a := stitch.Actor{Kind: "user", ID: u.String()}
	if _, err := s.Commit(ctx, u, p, 0, "title-after-freeze", a, "title", []stitch.Operation{{Type: "set_title", Title: "changed"}}); err != nil {
		t.Fatal(err)
	}
	snap, err = s.Get(ctx, u, p)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Document.Segments[0].SourceInUS != 100000000 || snap.Document.Segments[0].DurationUS != 10000000 {
		t.Fatalf("frozen metadata changed: %+v", snap.Document.Segments[0])
	}
}

func TestStitchStoreLifecycleAndBranching(t *testing.T) {
	d, u, p := openStitchTest(t)
	s := stitch.NewStore(d)
	ctx := context.Background()
	if _, err := s.Enable(ctx, u, p); err != nil {
		t.Fatal(err)
	}
	a := stitch.Actor{Kind: "user", ID: u.String(), Name: "Test"}
	set := func(key, title string, rev int64) (stitch.Result, error) {
		return s.Commit(ctx, u, p, rev, key, a, "set title", []stitch.Operation{{Type: "set_title", Title: title}})
	}
	r1, err := set("one", "human", 0)
	if err != nil || r1.Revision != 1 || r1.Document.Title != "human" {
		t.Fatalf("commit: %+v %v", r1, err)
	}
	r1b, err := set("one", "human", 0)
	if err != nil || r1b.EditID != r1.EditID {
		t.Fatalf("retry mismatch: %+v %v", r1b, err)
	}
	r2, err := set("two", "agent", 1)
	if err != nil || r2.Revision != 2 {
		t.Fatal(err)
	}
	r3, err := s.Undo(ctx, u, p, 2, "undo-one", a, "undo")
	if err != nil || r3.Revision != 3 || r3.Document.Title != "human" {
		t.Fatalf("undo: %+v %v", r3, err)
	}
	r4, err := s.Redo(ctx, u, p, 3, "redo-one", a, "redo")
	if err != nil || r4.Revision != 4 || r4.Document.Title != "agent" {
		t.Fatalf("redo: %+v %v", r4, err)
	}
	if _, err = s.Undo(ctx, u, p, 4, "undo-two", a, "undo"); err != nil {
		t.Fatal(err)
	}
	if _, err = set("three", "new", 5); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Redo(ctx, u, p, 6, "redo-cleared", a, "redo"); err == nil {
		t.Fatal("redo should be cleared")
	}
	h, err := s.History(ctx, u, p, 0, 20)
	if err != nil || len(h) < 5 || len(h[0].Operations) == 0 {
		t.Fatalf("history: %d %v", len(h), err)
	}
}

func TestStitchStoreOwnershipStaleAndConcurrentRetry(t *testing.T) {
	d, u, p := openStitchTest(t)
	s := stitch.NewStore(d)
	ctx := context.Background()
	if _, err := s.Enable(ctx, u, p); err != nil {
		t.Fatal(err)
	}
	other := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	a := stitch.Actor{Kind: "user", ID: u.String()}
	if _, err := s.Get(ctx, other, p); !errors.Is(err, stitch.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := s.History(ctx, other, p, 0, 10); !errors.Is(err, stitch.ErrNotFound) {
		t.Fatal(err)
	}
	op := []stitch.Operation{{Type: "set_title", Title: "shared"}}
	var wg sync.WaitGroup
	results := make(chan stitch.Result, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := s.Commit(ctx, u, p, 0, "same", a, "shared", op)
			results <- r
			errs <- e
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	var id pgtype.UUID
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	for r := range results {
		if !id.Valid {
			id = r.EditID
		} else if id != r.EditID {
			t.Fatal("different edit ids")
		}
	}
	if _, err := s.Commit(ctx, other, p, 0, "same", a, "shared", op); !errors.Is(err, stitch.ErrNotFound) {
		t.Fatalf("foreign retry leaked result: %v", err)
	}
	if _, err := s.Commit(ctx, u, p, 0, "stale", a, "stale", op); err == nil {
		t.Fatal("expected stale conflict")
	}
}

func TestStitchSourceBoundsRejectMismatchedAndOutOfBoundsAtomically(t *testing.T) {
	d, u, p := openStitchTest(t)
	ctx := context.Background()
	s := stitch.NewStore(d)
	if _, err := s.Enable(ctx, u, p); err != nil {
		t.Fatal(err)
	}
	v1, v2, clip := pgtype.UUID{Bytes: uuid.New(), Valid: true}, pgtype.UUID{Bytes: uuid.New(), Valid: true}, pgtype.UUID{Bytes: uuid.New(), Valid: true}
	for _, v := range []pgtype.UUID{v1, v2} {
		if _, err := d.Exec(ctx, `INSERT INTO videos(id,src,archived_by,title) VALUES($1,$2,$3,'source')`, v, "bounds-"+v.String(), u); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.Exec(ctx, `INSERT INTO clips(id,video_id,start_ts,end_ts,duration,created_by,title) VALUES($1,$2,10,20,10,$3,'clip')`, clip, v1, u); err != nil {
		t.Fatal(err)
	}
	a := stitch.Actor{Kind: "user", ID: u.String()}
	badVideo := stitch.Segment{ID: "bad-video", Type: "clip", ClipID: clip.String(), VideoID: v2.String(), SourceInUS: 10_000_000, DurationUS: 1_000_000}
	if _, err := s.Commit(ctx, u, p, 0, "bad-video", a, "bad", []stitch.Operation{{Type: "insert_segment", Segment: &badVideo}}); err == nil {
		t.Fatal("expected mismatched video rejection")
	}
	badRange := stitch.Segment{ID: "bad-range", Type: "clip", ClipID: clip.String(), SourceInUS: 19_000_000, DurationUS: 2_000_000}
	forged := stitch.Segment{ID: "forged", Type: "clip", ClipID: clip.String(), SourceInUS: 999_000_000, DurationUS: 1_000_000, Legacy: json.RawMessage(`{"legacy_metadata":{"video_id":"` + v2.String() + `","start_us":0,"duration_us":9999999999,"end_ts":99999}}`)}
	if _, err := s.Commit(ctx, u, p, 0, "forged-meta", a, "bad", []stitch.Operation{{Type: "insert_segment", Segment: &forged}}); err == nil {
		t.Fatal("expected forged metadata rejection")
	}
	if _, err := s.Commit(ctx, u, p, 0, "bad-range", a, "bad", []stitch.Operation{{Type: "insert_segment", Segment: &badRange}}); err == nil {
		t.Fatal("expected out-of-bounds rejection")
	}
	snap, err := s.Get(ctx, u, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Document.Segments) != 0 || snap.Revision != 0 {
		t.Fatalf("atomicity lost: %+v", snap)
	}
}

func TestStitchCompilationRenderLocksOwnerRevisionAndIdempotency(t *testing.T) {
	d, u, p := openStitchTest(t)
	ctx := context.Background()
	s := stitch.NewStore(d)
	if _, err := s.Enable(ctx, u, p); err != nil {
		t.Fatal(err)
	}
	actor := stitch.Actor{Kind: "user", ID: u.String()}
	if _, err := s.Commit(ctx, u, p, 0, "seed-render", actor, "seed", []stitch.Operation{{Type: "insert_segment", Segment: &stitch.Segment{ID: "title", Type: "title", DurationUS: 1_000_000}}}); err != nil {
		t.Fatal(err)
	}
	snap, err := s.Get(ctx, u, p)
	if err != nil {
		t.Fatal(err)
	}
	queue := func(owner pgtype.UUID, rev int64, key string, title string) (pgtype.UUID, error) {
		tx, err := d.Pool.Begin(ctx)
		if err != nil {
			return pgtype.UUID{}, err
		}
		id, err := stitch.QueueCompilationRender(ctx, tx, owner, p, rev, key, stitch.Document{Version: stitch.CurrentVersion, Title: title, Segments: []stitch.Segment{{ID: "forged", Type: "title", DurationUS: 1_000_000}}})
		if err != nil {
			_ = tx.Rollback(ctx)
			return id, err
		}
		return id, tx.Commit(ctx)
	}
	id, err := queue(u, snap.Revision, "compile-key", "ignored caller")
	if err != nil {
		t.Fatal(err)
	}
	retry, err := queue(u, snap.Revision, "compile-key", "different caller")
	if err != nil || retry != id {
		t.Fatalf("retry=%s err=%v", retry, err)
	}
	var captured []byte
	if err := d.QueryRow(ctx, `SELECT document_snapshot FROM stitch_jobs WHERE id=$1`, id).Scan(&captured); err != nil {
		t.Fatal(err)
	}
	var snapshot stitch.RenderSnapshot
	if err := json.Unmarshal(captured, &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Document.Segments) != 1 || snapshot.Document.Segments[0].ID != "title" {
		t.Fatalf("snapshot trusted caller document: %s", captured)
	}
	advanced, err := s.Commit(ctx, u, p, snap.Revision, "later-compilation-edit", actor, "Later edit", []stitch.Operation{{Type: "set_title", Title: "Later title"}})
	if err != nil {
		t.Fatal(err)
	}
	retry, err = queue(u, snap.Revision, "compile-key", "ignored")
	if err != nil || retry != id {
		t.Fatalf("retry after edit: %s %v", retry, err)
	}
	if _, err := queue(u, advanced.Revision, "compile-key", "ignored"); !errors.Is(err, stitch.ErrIdempotency) {
		t.Fatalf("changed request key reuse: %v", err)
	}
	foreign := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	if _, err := queue(foreign, snap.Revision, "foreign-key", "x"); !errors.Is(err, stitch.ErrNotFound) {
		t.Fatalf("foreign=%v", err)
	}
	if _, err := queue(u, snap.Revision-1, "stale-key", "x"); !errors.Is(err, stitch.ErrConflict) {
		t.Fatalf("stale=%v", err)
	}
}

func TestStitchYouTubeMetadata(t *testing.T) {
	d, u, p := openStitchTest(t)
	s := stitch.NewStore(d)
	ctx := context.Background()
	if _, err := s.Enable(ctx, u, p); err != nil {
		t.Fatal(err)
	}
	before, err := s.Get(ctx, u, p)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := s.SetYouTube(ctx, u, p, "  upload blurb  ", []string{"Alpha", " alpha ", "", "Beta"})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Revision != before.Revision {
		t.Fatalf("revision changed: %d -> %d", before.Revision, snap.Revision)
	}
	if snap.Description != "upload blurb" {
		t.Fatalf("description=%q", snap.Description)
	}
	if len(snap.Tags) != 2 || snap.Tags[0] != "Alpha" || snap.Tags[1] != "Beta" {
		t.Fatalf("tags=%v", snap.Tags)
	}
	got, err := s.Get(ctx, u, p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != before.Revision || got.Description != "upload blurb" || len(got.Tags) != 2 {
		t.Fatalf("get mismatch: %+v", got)
	}
	_, other, _ := openStitchTest(t)
	if _, err := s.SetYouTube(ctx, other, p, "nope", []string{"x"}); !errors.Is(err, stitch.ErrNotFound) {
		t.Fatalf("foreign owner: %v", err)
	}
	got, err = s.Get(ctx, u, p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != "upload blurb" {
		t.Fatalf("foreign write persisted: %q", got.Description)
	}
	long := strings.Repeat("x", stitch.MaxYouTubeDescription+1)
	if _, err := s.SetYouTube(ctx, u, p, long, nil); err == nil {
		t.Fatal("expected over-long description error")
	}
	got, err = s.Get(ctx, u, p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != "upload blurb" || got.Revision != before.Revision {
		t.Fatalf("over-long persisted: %+v", got)
	}
}
