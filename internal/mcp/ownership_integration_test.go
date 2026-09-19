//go:build integration

package mcp

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"testing"
	"thirdcoast.systems/rewind/internal/application"
	"thirdcoast.systems/rewind/internal/config"
	"thirdcoast.systems/rewind/internal/db"
	"time"
)

func TestMCPAccountAndFollowOwnership(t *testing.T) {
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
	owner, other := pgtype.UUID{Bytes: uuid.New(), Valid: true}, pgtype.UUID{Bytes: uuid.New(), Valid: true}
	for _, id := range []pgtype.UUID{owner, other} {
		if _, err = pool.Exec(ctx, "INSERT INTO users(id,user_name,email,password,enabled) VALUES($1,$2,$2,'fixture',true)", id, id.String()); err != nil {
			t.Fatal(err)
		}
	}
	plain := uuid.NewString()
	q := dbc.Queries(ctx)
	if _, err = q.InsertAPIToken(ctx, &db.InsertAPITokenParams{UserID: owner, Name: "fixture", TokenHash: HashToken(plain), Scopes: []string{"mcp:read", "mcp:write"}}); err != nil {
		t.Fatal(err)
	}
	if _, err = Authenticate(ctx, dbc, "Bearer "+plain); err != nil {
		t.Fatal(err)
	}
	watch, err := q.CreateWatchedChannel(ctx, &db.CreateWatchedChannelParams{CreatedBy: owner, URL: "https://example.invalid/" + uuid.NewString(), CronSchedule: "@daily", NextScanAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}})
	if err != nil {
		t.Fatal(err)
	}
	foreign := withToken(ctx, &db.APIToken{UserID: other, Scopes: []string{"mcp:read", "mcp:write"}})
	for _, args := range []*unfollowArgs{{ID: watch.ID.String()}, {URL: watch.URL}} {
		if _, _, err = unfollowChannel(dbc)(foreign, nil, args); err == nil {
			t.Fatal("foreign user removed follow")
		}
	}
	owned := withToken(ctx, &db.APIToken{UserID: owner, Scopes: []string{"mcp:read", "mcp:write"}})
	if _, _, err = unfollowChannel(dbc)(owned, nil, &unfollowArgs{ID: watch.ID.String()}); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "UPDATE users SET enabled=false WHERE id=$1", owner); err != nil {
		t.Fatal(err)
	}
	if _, err = Authenticate(ctx, dbc, "Bearer "+plain); err == nil {
		t.Fatal("disabled account authenticated")
	}
}
