package common

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/plugin"
	"thirdcoast.systems/rewind/pkg/plugin/builtin"
)

type fakeLookup struct {
	byID        *db.Video
	byIDErr     error
	byTenant    *db.Video
	byTenantErr error
	tenantCalls int
	idCalls     int
}

func (f *fakeLookup) GetVideoByID(context.Context, pgtype.UUID) (*db.Video, error) {
	f.idCalls++
	return f.byID, f.byIDErr
}

func (f *fakeLookup) GetVideoByIDAndTenant(context.Context, *db.GetVideoByIDAndTenantParams) (*db.Video, error) {
	f.tenantCalls++
	return f.byTenant, f.byTenantErr
}

func TestGetVideoOSSUsesIDLookup(t *testing.T) {
	id := pgtype.UUID{Valid: true}
	id.Bytes[0] = 1
	want := &db.Video{ID: id, Title: "oss"}
	q := &fakeLookup{byID: want}
	got, err := GetVideo(context.Background(), q, id, "")
	if err != nil || got != want {
		t.Fatalf("got %+v err %v", got, err)
	}
	if q.tenantCalls != 0 || q.idCalls != 1 {
		t.Fatalf("calls id=%d tenant=%d", q.idCalls, q.tenantCalls)
	}
}

func TestGetVideoTenantFailClosed(t *testing.T) {
	id := pgtype.UUID{Valid: true}
	id.Bytes[0] = 1
	other := &db.Video{ID: id, Title: "other"}
	q := &fakeLookup{byID: other, byTenantErr: errors.New("no rows")}
	tenant := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	got, err := GetVideo(context.Background(), q, id, tenant)
	if err == nil || got != nil {
		t.Fatalf("want fail closed, got %+v err %v", got, err)
	}
	if q.idCalls != 0 || q.tenantCalls != 1 {
		t.Fatalf("must not use unscoped lookup: id=%d tenant=%d", q.idCalls, q.tenantCalls)
	}
}

func TestGetVideoInvalidTenantFailClosed(t *testing.T) {
	id := pgtype.UUID{Valid: true}
	q := &fakeLookup{byID: &db.Video{ID: id}}
	_, err := GetVideo(context.Background(), q, id, "not-a-uuid")
	if err == nil {
		t.Fatal("expected invalid tenant")
	}
	if q.idCalls != 0 {
		t.Fatal("must not fall open to GetVideoByID")
	}
}

type testAuthn struct {
	actor *plugin.Actor
	err   error
}

func (t testAuthn) Current(*http.Request) (*plugin.Actor, error) { return t.actor, t.err }
func (testAuthn) Login(http.ResponseWriter, *http.Request) error  { return plugin.ErrNotSupported }
func (testAuthn) Logout(http.ResponseWriter, *http.Request) error { return nil }
func (testAuthn) Register(http.ResponseWriter, *http.Request) error {
	return plugin.ErrNotSupported
}
func (testAuthn) LoginPath() string { return "/login" }
func (testAuthn) Mount(*echo.Echo)  {}

type denyAuthz struct{}

func (denyAuthz) Allow(context.Context, *plugin.Actor, string, string) bool { return false }

func echoCtx() echo.Context {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec)
}

func httpErrStatus(err error) int {
	var he *echo.HTTPError
	if errors.As(err, &he) {
		return he.Code
	}
	return 0
}

func TestRequireVideoDenyWithTenantSkipsUnscopedLookup(t *testing.T) {
	plugin.Reset()
	t.Cleanup(plugin.Reset)

	id := pgtype.UUID{Valid: true}
	id.Bytes[0] = 1
	tenant := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	q := &fakeLookup{byID: &db.Video{ID: id, Title: "leak"}, byTenant: &db.Video{ID: id, Title: "tenant"}}
	plugin.Use(plugin.Set{
		Authn: testAuthn{actor: &plugin.Actor{UserID: "u1", TenantID: tenant}},
		Authz: denyAuthz{},
	})

	_, err := RequireVideo(echoCtx(), q, id, plugin.ActionVideoRead)
	if httpErrStatus(err) != http.StatusNotFound {
		t.Fatalf("want 404, got %v", err)
	}
	if q.idCalls != 0 {
		t.Fatalf("must not call GetVideoByID when tenant set: id=%d", q.idCalls)
	}
	if q.tenantCalls != 1 {
		t.Fatalf("want tenant lookup before deny: tenant=%d", q.tenantCalls)
	}
}

func TestRequireVideoDenyEmptyTenantUsesByIDThen404(t *testing.T) {
	plugin.Reset()
	t.Cleanup(plugin.Reset)

	id := pgtype.UUID{Valid: true}
	id.Bytes[0] = 2
	want := &db.Video{ID: id, Title: "oss"}
	q := &fakeLookup{byID: want}
	plugin.Use(plugin.Set{
		Authn: testAuthn{actor: &plugin.Actor{UserID: "u1", TenantID: ""}},
		Authz: denyAuthz{},
	})

	got, err := RequireVideo(echoCtx(), q, id, plugin.ActionVideoRead)
	if got != nil || httpErrStatus(err) != http.StatusNotFound {
		t.Fatalf("want 404 nil video, got %+v err %v", got, err)
	}
	if q.idCalls != 1 || q.tenantCalls != 0 {
		t.Fatalf("empty tenant uses by-id: id=%d tenant=%d", q.idCalls, q.tenantCalls)
	}
}

func TestRequireVideoInvalidTenant(t *testing.T) {
	plugin.Reset()
	t.Cleanup(plugin.Reset)

	id := pgtype.UUID{Valid: true}
	q := &fakeLookup{byID: &db.Video{ID: id}}
	plugin.Use(plugin.Set{
		Authn: testAuthn{actor: &plugin.Actor{UserID: "u1", TenantID: "not-a-uuid"}},
	})

	_, err := RequireVideo(echoCtx(), q, id, plugin.ActionVideoRead)
	if httpErrStatus(err) != http.StatusNotFound {
		t.Fatalf("want 404, got %v", err)
	}
	if q.idCalls != 0 {
		t.Fatal("must not fall open to GetVideoByID")
	}
}

func TestWithActorTenantEmptyValidInvalid(t *testing.T) {
	plugin.Reset()
	t.Cleanup(plugin.Reset)

	c := echoCtx()
	p := &db.ListVideosPaginatedParams{}
	WithActorTenant(c, p)
	if p.TenantID.Valid {
		t.Fatalf("no actor: TenantID should stay unset, got %+v", p.TenantID)
	}

	plugin.Use(plugin.Set{
		Authn: testAuthn{actor: &plugin.Actor{UserID: "u1", TenantID: ""}},
	})
	c = echoCtx()
	p = &db.ListVideosPaginatedParams{}
	WithActorTenant(c, p)
	if p.TenantID.Valid {
		t.Fatalf("empty tenant: leave unset, got %+v", p.TenantID)
	}

	plugin.Reset()
	valid := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	plugin.Use(plugin.Set{
		Authn: testAuthn{actor: &plugin.Actor{UserID: "u1", TenantID: valid}},
	})
	c = echoCtx()
	p = &db.ListVideosPaginatedParams{}
	WithActorTenant(c, p)
	want := db.ParseTenant(valid)
	if p.TenantID != want {
		t.Fatalf("valid tenant: got %+v want %+v", p.TenantID, want)
	}

	plugin.Reset()
	plugin.Use(plugin.Set{
		Authn: testAuthn{actor: &plugin.Actor{UserID: "u1", TenantID: "not-a-uuid"}},
	})
	c = echoCtx()
	p = &db.ListVideosPaginatedParams{}
	WithActorTenant(c, p)
	var dead pgtype.UUID
	_ = dead.Scan("ffffffff-ffff-ffff-ffff-ffffffffffff")
	if p.TenantID != dead {
		t.Fatalf("invalid tenant fail-closed: got %+v want %+v", p.TenantID, dead)
	}

	WithActorTenant(echoCtx(), nil) // nil params must not panic
}

type allowAuthz struct{}

func (allowAuthz) Allow(context.Context, *plugin.Actor, string, string) bool { return true }

func TestWithActorTenantLiveGuardsFailClosed(t *testing.T) {
	plugin.Reset()
	t.Cleanup(plugin.Reset)

	dead := deadTenant()

	plugin.Use(plugin.Set{
		Authn: testAuthn{actor: &plugin.Actor{UserID: "u1", TenantID: "", Roles: []string{"user"}}},
		Authz: allowAuthz{},
	})
	p := &db.ListVideosPaginatedParams{}
	WithActorTenant(echoCtx(), p)
	if p.TenantID != dead {
		t.Fatalf("live guards empty tenant non-admin: got %+v want dead", p.TenantID)
	}

	plugin.Reset()
	plugin.Use(plugin.Set{
		Authn: testAuthn{actor: &plugin.Actor{UserID: "u1", TenantID: "", Roles: []string{"user", "admin"}}},
		Authz: allowAuthz{},
	})
	p = &db.ListVideosPaginatedParams{}
	WithActorTenant(echoCtx(), p)
	if p.TenantID.Valid {
		t.Fatalf("admin empty tenant should stay unscoped, got %+v", p.TenantID)
	}

	plugin.Reset()
	plugin.Use(plugin.Set{
		Authn: testAuthn{actor: &plugin.Actor{UserID: "u1", TenantID: "", Roles: []string{"user"}}},
		Authz: builtin.LocalAuthz{},
	})
	p = &db.ListVideosPaginatedParams{}
	WithActorTenant(echoCtx(), p)
	if p.TenantID.Valid {
		t.Fatalf("OSS LocalAuthz should stay unscoped, got %+v", p.TenantID)
	}
}
