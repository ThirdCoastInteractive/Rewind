//go:build integration

package topics

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/application"
	"thirdcoast.systems/rewind/internal/config"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/plugin"
)

func TestTenantTopicCatalogAndBinds(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}
	ctx := context.Background()
	pool, err := application.OpenDBPoolWithRetry(ctx, config.Config{DatabaseDSN: dsn, DatabaseRetries: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	dbc := &db.DatabaseConnection{Pool: pool}
	if err := dbc.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	plugin.Reset()
	tenantA := pgtype.UUID{Bytes: uuid.MustParse("33333333-3333-3333-3333-333333333333"), Valid: true}
	tenantB := pgtype.UUID{Bytes: uuid.MustParse("44444444-4444-4444-4444-444444444444"), Valid: true}
	owner := pgtype.UUID{Bytes: uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"), Valid: true}
	if _, err := pool.Exec(ctx, `INSERT INTO users (id,user_name,password,email) VALUES ($1,'topic-fixture-owner',''::hashed_password,'topic-fixture-owner@example.invalid') ON CONFLICT (id) DO NOTHING`, owner); err != nil {
		t.Fatal(err)
	}
	q := dbc.Queries(ctx)
	slug := "shared-topic-" + uuid.NewString()
	for _, tc := range []struct {
		tenant pgtype.UUID
		title  string
	}{{tenantA, "Alpha Topic"}, {tenantB, "Beta Topic"}} {
		if _, err := q.UpsertTopic(ctx, &db.UpsertTopicParams{TenantID: tc.tenant, Slug: slug, Title: tc.title, Origin: "wiki"}); err != nil {
			t.Fatal(err)
		}
		if err := q.InsertTopicAlias(ctx, &db.InsertTopicAliasParams{TenantID: tc.tenant, AliasNorm: slug, TopicSlug: slug, Raw: "Shared Alias", Source: "wiki_title"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		tenant pgtype.UUID
		src    string
	}{{tenantA, "topic-a-" + uuid.NewString()}, {tenantB, "topic-b-" + uuid.NewString()}} {
		video := uuid.New()
		if _, err := pool.Exec(ctx, `INSERT INTO videos (id,src,archived_by,title,tenant_id) VALUES ($1,$2,$3,'topic fixture',$4)`, video, tc.src, owner, tc.tenant); err != nil {
			t.Fatal(err)
		}
		window := uuid.New()
		if _, err := pool.Exec(ctx, `INSERT INTO context_windows (id,video_id,start_ts,end_ts,title,created_by) VALUES ($1,$2,0,10,'window',$3)`, window, video, owner); err != nil {
			t.Fatal(err)
		}
		c := plugin.WithTenantScope(ctx, tc.tenant.String(), true)
		if err := New(dbc).BindWindow(c, pgtype.UUID{Bytes: window, Valid: true}, "window", []string{slug}, nil); err != nil {
			t.Fatal(err)
		}
		count, err := q.CountTopicWindows(c, &db.CountTopicWindowsParams{TenantID: tc.tenant, Slug: slug})
		if err != nil || count != 1 {
			t.Fatalf("tenant %s bind count=%d err=%v", tc.tenant, count, err)
		}
		binds, err := q.ListWindowTopicBinds(c, []pgtype.UUID{{Bytes: window, Valid: true}})
		if err != nil || len(binds) != 1 {
			t.Fatalf("tenant %s binds=%d err=%v", tc.tenant, len(binds), err)
		}
		prompt := CatalogPrompt(c, q)
		want := "Alpha Topic"
		if tc.tenant == tenantB {
			want = "Beta Topic"
		}
		if !containsTopic(prompt, want) {
			t.Fatalf("tenant %s prompt=%q", tc.tenant, prompt)
		}
		other := "Beta Topic"
		if tc.tenant == tenantB {
			other = "Alpha Topic"
		}
		if containsTopic(prompt, other) {
			t.Fatalf("tenant %s leaked %q", tc.tenant, other)
		}
	}
}

func containsTopic(prompt, title string) bool { return strings.Contains(prompt, "- "+title+"\n") }
