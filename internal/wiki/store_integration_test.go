//go:build integration

package wiki

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/application"
	"thirdcoast.systems/rewind/internal/config"
	"thirdcoast.systems/rewind/internal/db"
)

func testStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	ctx := context.Background()
	pool, err := application.OpenDBPoolWithRetry(ctx, config.Config{
		DatabaseDSN:     "postgres://rewind_test:disposable-test-only@127.0.0.1:15439/rewind_test?sslmode=disable",
		DatabaseRetries: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	dbc := &db.DatabaseConnection{Pool: pool}
	if err := dbc.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return New(dbc), ctx
}

func TestPutCreateUpdateAndLinks(t *testing.T) {
	store, ctx := testStore(t)
	slug := "fixture-" + uuid.NewString()
	workflow := "workflow-" + uuid.NewString()
	actor := Actor{Kind: "agent", ID: "test-agent"}

	created, err := store.Put(ctx, PutInput{
		Tree: TreeTopic, Slug: slug, Title: "Index",
		Body: "See [[clipping/" + workflow + "]] and [[notes]].", Summary: "seed page",
		ExpectedRevision: 0, Actor: actor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Revision != 1 {
		t.Fatalf("revision=%d", created.Revision)
	}

	got, err := store.Get(ctx, TreeTopic, slug)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Links) != 2 {
		t.Fatalf("links=%#v", got.Links)
	}

	// Create the linked page so backlinks resolve from a real tip.
	_, err = store.Put(ctx, PutInput{
		Tree: TreeClipping, Slug: workflow, Title: "Workflow",
		Body: "Parent [[topic/" + slug + "]].", Summary: "link back",
		ExpectedRevision: 0, Actor: actor,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err = store.Get(ctx, TreeTopic, slug)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Backlinks) != 1 || got.Backlinks[0].Slug != workflow {
		t.Fatalf("backlinks=%#v", got.Backlinks)
	}

	updated, err := store.Put(ctx, PutInput{
		Tree: TreeTopic, Slug: slug, Title: "Index",
		Body: "Updated body only.\n", Summary: "tweak body",
		ExpectedRevision: 1, Actor: actor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision != 2 {
		t.Fatalf("revision=%d", updated.Revision)
	}

	hist, err := store.History(ctx, TreeTopic, slug)
	if err != nil || len(hist) != 2 {
		t.Fatalf("history=%d err=%v", len(hist), err)
	}
	if hist[0].Diff == "" || hist[1].Diff != "" {
		t.Fatalf("rev1 diff empty=%t rev2 empty=%t", hist[1].Diff == "", hist[0].Diff == "")
	}

	diff, err := store.Diff(ctx, TreeTopic, slug, 0, 0)
	if err != nil || diff == "" {
		t.Fatalf("diff=%q err=%v", diff, err)
	}
}

func TestPutStaleRevision(t *testing.T) {
	store, ctx := testStore(t)
	slug := "stale-" + uuid.NewString()
	actor := Actor{Kind: "user", ID: "u1", UserID: pgtype.UUID{Bytes: uuid.New(), Valid: false}}

	page, err := store.Put(ctx, PutInput{
		Tree: TreeCreator, Slug: slug, Title: "A", Body: "one", Summary: "create",
		ExpectedRevision: 0, Actor: actor,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.Put(ctx, PutInput{
		Tree: TreeCreator, Slug: slug, Title: "A", Body: "two", Summary: "ok",
		ExpectedRevision: page.Revision, Actor: actor,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.Put(ctx, PutInput{
		Tree: TreeCreator, Slug: slug, Title: "A", Body: "three", Summary: "stale",
		ExpectedRevision: page.Revision, Actor: actor,
	})
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("want revision conflict, got %v", err)
	}

	_, err = store.Put(ctx, PutInput{
		Tree: TreeCreator, Slug: "missing-" + uuid.NewString(), Title: "X", Body: "x", Summary: "bad",
		ExpectedRevision: 1, Actor: actor,
	})
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("want conflict on missing+expected=1, got %v", err)
	}
}

func TestPutRejectsBlankSummary(t *testing.T) {
	store, ctx := testStore(t)
	_, err := store.Put(ctx, PutInput{
		Tree: TreeTopic, Slug: "x", Title: "X", Body: "b", Summary: "  ",
		ExpectedRevision: 0, Actor: Actor{Kind: "system", ID: "seed"},
	})
	if err == nil {
		t.Fatal("expected summary error")
	}
}
