//go:build integration

package shownote

import (
	"context"
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

func TestShowNoteTenantIsolation(t *testing.T) {
	ctx := context.Background()
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}
	pool, err := application.OpenDBPoolWithRetry(ctx, config.Config{DatabaseDSN: dsn, DatabaseRetries: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	dbc := &db.DatabaseConnection{Pool: pool}
	if err := dbc.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	plugin.Use(plugin.Set{Live: tenantTestLive{}})
	t.Cleanup(plugin.Reset)
	owner := pgtype.UUID{Bytes: uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"), Valid: true}
	_, err = pool.Exec(ctx, `INSERT INTO users (id,user_name,password,email) VALUES ($1,$2,''::hashed_password,$3) ON CONFLICT (id) DO NOTHING`, owner, "tenant-test-owner", "tenant-test-owner@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	tenantA := pgtype.UUID{Bytes: uuid.MustParse("11111111-1111-1111-1111-111111111111"), Valid: true}
	tenantB := pgtype.UUID{Bytes: uuid.MustParse("22222222-2222-2222-2222-222222222222"), Valid: true}
	q := dbc.Queries(ctx)
	a, err := q.CreateShowNote(ctx, &db.CreateShowNoteParams{OwnerID: owner, Title: "same", TenantID: tenantA})
	if err != nil {
		t.Fatal(err)
	}
	b, err := q.CreateShowNote(ctx, &db.CreateShowNoteParams{OwnerID: owner, Title: "same", TenantID: tenantB})
	if err != nil {
		t.Fatal(err)
	}
	ctxA := plugin.WithTenantScope(ctx, tenantA.String(), true)
	ctxB := plugin.WithTenantScope(ctx, tenantB.String(), true)
	if _, err := RequireTenant(ctxA, dbc, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := RequireTenant(ctxB, dbc, a.ID); err == nil {
		t.Fatal("cross-tenant note accepted")
	}
	if _, err := RequireTenant(ctxA, dbc, b.ID); err == nil {
		t.Fatal("tenant B note accepted by tenant A")
	}
}

type tenantTestLive struct{}

func (tenantTestLive) CreateInput(context.Context, *plugin.Actor, string) (*plugin.LiveInput, error) {
	return nil, plugin.ErrNotSupported
}
func (tenantTestLive) GetInput(context.Context, *plugin.Actor, string) (*plugin.LiveInput, error) {
	return nil, plugin.ErrNotSupported
}
func (tenantTestLive) ListInputs(context.Context, *plugin.Actor) ([]*plugin.LiveInput, error) {
	return nil, plugin.ErrNotSupported
}
func (tenantTestLive) DeleteInput(context.Context, *plugin.Actor, string) error {
	return plugin.ErrNotSupported
}
func (tenantTestLive) AddOutput(context.Context, *plugin.Actor, string, string, string) (*plugin.LiveOutput, error) {
	return nil, plugin.ErrNotSupported
}
func (tenantTestLive) RemoveOutput(context.Context, *plugin.Actor, string, string) error {
	return plugin.ErrNotSupported
}
func (tenantTestLive) EnableOutput(context.Context, *plugin.Actor, string, string, bool) error {
	return plugin.ErrNotSupported
}
func (tenantTestLive) Mount(*echo.Echo) {}
