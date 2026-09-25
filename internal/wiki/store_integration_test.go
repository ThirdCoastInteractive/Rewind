//go:build integration

package wiki

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/internal/application"
	"thirdcoast.systems/rewind/internal/config"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/plugin"
)

func testStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	ctx := context.Background()
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}
	pool, err := application.OpenDBPoolWithRetry(ctx, config.Config{
		DatabaseDSN:     dsn,
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

func TestTenantIsolationAcrossPagesSearchHistoryAndBacklinks(t *testing.T) {
	base, ctx := testStore(t)
	tenantA, tenantB := uuid.New(), uuid.New()
	a := NewForTenant(base.db, pgtype.UUID{Bytes: tenantA, Valid: true})
	b := NewForTenant(base.db, pgtype.UUID{Bytes: tenantB, Valid: true})
	actor := Actor{Kind: "user", ID: "tenant-test"}
	for _, tc := range []struct {
		store *Store
		title string
	}{{a, "A secret"}, {b, "B secret"}} {
		if _, err := tc.store.Put(ctx, PutInput{Tree: TreeTopic, Slug: "same", Title: tc.title, Body: "[[topic/target]]", Summary: "create", Actor: actor}); err != nil {
			t.Fatal(err)
		}
		if _, err := tc.store.Put(ctx, PutInput{Tree: TreeTopic, Slug: "target", Title: tc.title + " target", Body: "target", Summary: "target", Actor: actor}); err != nil {
			t.Fatal(err)
		}
	}
	gotA, err := a.Get(ctx, TreeTopic, "same")
	if err != nil || gotA.Title != "A secret" || len(gotA.Backlinks) != 0 {
		t.Fatalf("tenant A page=%+v err=%v", gotA, err)
	}
	gotB, err := b.Get(ctx, TreeTopic, "same")
	if err != nil || gotB.Title != "B secret" {
		t.Fatalf("tenant B page=%+v err=%v", gotB, err)
	}
	history, err := a.History(ctx, TreeTopic, "same")
	if err != nil || len(history) != 1 {
		t.Fatalf("tenant A history=%d err=%v", len(history), err)
	}
	hits, err := a.Search(ctx, "secret", TreeTopic, 20)
	if err != nil || len(hits) != 2 {
		t.Fatalf("tenant A search=%d err=%v", len(hits), err)
	}
	if _, err := b.Get(ctx, TreeTopic, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing tenant page err=%v", err)
	}
}

func TestLiveWikiScopeRejectsMissingAndZeroTenant(t *testing.T) {
	base, _ := testStore(t)
	plugin.Use(plugin.Set{Live: pluginTestLive{}})
	t.Cleanup(plugin.Reset)
	for _, tenant := range []string{"", "00000000-0000-0000-0000-000000000000"} {
		ctx := plugin.WithTenantScope(context.Background(), tenant, true)
		_, err := base.Put(ctx, PutInput{Tree: TreeTopic, Slug: "blocked", Title: "x", Body: "x", Summary: "x", Actor: Actor{Kind: "system"}})
		if !errors.Is(err, ErrTenantRequired) {
			t.Fatalf("tenant %q write err=%v", tenant, err)
		}
		if _, err := base.Get(ctx, TreeTopic, "blocked"); !errors.Is(err, ErrTenantRequired) {
			t.Fatalf("tenant %q read err=%v", tenant, err)
		}
	}
}

type pluginTestLive struct{}

func (pluginTestLive) CreateInput(context.Context, *plugin.Actor, string) (*plugin.LiveInput, error) {
	return nil, plugin.ErrNotSupported
}
func (pluginTestLive) GetInput(context.Context, *plugin.Actor, string) (*plugin.LiveInput, error) {
	return nil, plugin.ErrNotSupported
}
func (pluginTestLive) ListInputs(context.Context, *plugin.Actor) ([]*plugin.LiveInput, error) {
	return nil, plugin.ErrNotSupported
}
func (pluginTestLive) DeleteInput(context.Context, *plugin.Actor, string) error {
	return plugin.ErrNotSupported
}
func (pluginTestLive) AddOutput(context.Context, *plugin.Actor, string, string, string) (*plugin.LiveOutput, error) {
	return nil, plugin.ErrNotSupported
}
func (pluginTestLive) RemoveOutput(context.Context, *plugin.Actor, string, string) error {
	return plugin.ErrNotSupported
}
func (pluginTestLive) EnableOutput(context.Context, *plugin.Actor, string, string, bool) error {
	return plugin.ErrNotSupported
}
func (pluginTestLive) Mount(*echo.Echo) {}
