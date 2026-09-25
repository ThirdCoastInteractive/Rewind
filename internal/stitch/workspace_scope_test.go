package stitch

import (
	"context"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/pkg/plugin"
)

type scopeAuth struct{ workspace string }

func (a *scopeAuth) WorkspaceForUser(context.Context, string) (string, error) {
	return a.workspace, nil
}
func (*scopeAuth) Current(*http.Request) (*plugin.Actor, error)      { return nil, nil }
func (*scopeAuth) Login(http.ResponseWriter, *http.Request) error    { return nil }
func (*scopeAuth) Logout(http.ResponseWriter, *http.Request) error   { return nil }
func (*scopeAuth) Register(http.ResponseWriter, *http.Request) error { return nil }
func (*scopeAuth) LoginPath() string                                 { return "/login" }
func (*scopeAuth) Mount(*echo.Echo)                                  {}

type scopeLive struct{}

func (*scopeLive) CreateInput(context.Context, *plugin.Actor, string) (*plugin.LiveInput, error) {
	return nil, nil
}
func (*scopeLive) GetInput(context.Context, *plugin.Actor, string) (*plugin.LiveInput, error) {
	return nil, nil
}
func (*scopeLive) ListInputs(context.Context, *plugin.Actor) ([]*plugin.LiveInput, error) {
	return nil, nil
}
func (*scopeLive) DeleteInput(context.Context, *plugin.Actor, string) error { return nil }
func (*scopeLive) AddOutput(context.Context, *plugin.Actor, string, string, string) (*plugin.LiveOutput, error) {
	return nil, nil
}
func (*scopeLive) RemoveOutput(context.Context, *plugin.Actor, string, string) error { return nil }
func (*scopeLive) EnableOutput(context.Context, *plugin.Actor, string, string, bool) error {
	return nil
}
func (*scopeLive) Mount(*echo.Echo) {}

func TestWorkspaceTenantRejectsZeroScope(t *testing.T) {
	plugin.Reset()
	defer plugin.Reset()
	plugin.Use(plugin.Set{Authn: &scopeAuth{workspace: "00000000-0000-0000-0000-000000000000"}, Live: &scopeLive{}})
	var owner pgtype.UUID
	if err := owner.Scan("11111111-1111-1111-1111-111111111111"); err != nil {
		t.Fatal(err)
	}
	if _, enforce, err := workspaceTenant(context.Background(), owner); !enforce || err == nil {
		t.Fatalf("zero workspace must fail closed: enforce=%v err=%v", enforce, err)
	}
}

func TestWorkspaceTenantAcceptsTrustedScope(t *testing.T) {
	plugin.Reset()
	defer plugin.Reset()
	plugin.Use(plugin.Set{Authn: &scopeAuth{workspace: "22222222-2222-2222-2222-222222222222"}, Live: &scopeLive{}})
	var owner pgtype.UUID
	if err := owner.Scan("11111111-1111-1111-1111-111111111111"); err != nil {
		t.Fatal(err)
	}
	tenant, enforce, err := workspaceTenant(context.Background(), owner)
	if err != nil || !enforce || tenant.String() != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("trusted workspace rejected: tenant=%s enforce=%v err=%v", tenant, enforce, err)
	}
}
