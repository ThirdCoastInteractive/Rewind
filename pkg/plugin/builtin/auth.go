package builtin

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/pkg/plugin"
)

// LocalAuth wraps Rewind's cookie session. TenantID is always empty.
type LocalAuth struct {
	Sessions *auth.SessionManager
}

func NewLocalAuth(sessions *auth.SessionManager) *LocalAuth {
	return &LocalAuth{Sessions: sessions}
}

func (a *LocalAuth) Current(r *http.Request) (*plugin.Actor, error) {
	uid, name, level, _, err := a.Sessions.ReadCookie(r)
	if err != nil {
		return nil, plugin.ErrNotAuthenticated
	}
	roles := []string{"user"}
	if level == auth.AccessAdmin {
		roles = []string{"user", "admin"}
	}
	return &plugin.Actor{UserID: uid, Name: name, Roles: roles}, nil
}

func (a *LocalAuth) Login(http.ResponseWriter, *http.Request) error {
	return plugin.ErrNotSupported // existing /login handler stays the OSS path
}

func (a *LocalAuth) Logout(w http.ResponseWriter, r *http.Request) error {
	return a.Sessions.ClearSession(w, r)
}

func (a *LocalAuth) Register(http.ResponseWriter, *http.Request) error {
	return plugin.ErrNotSupported
}

func (a *LocalAuth) LoginPath() string { return "/login" }

func (a *LocalAuth) Mount(*echo.Echo) {}

// LocalAuthz: any logged-in user can read/write the library; admin is a role.
type LocalAuthz struct{}

func (LocalAuthz) Allow(_ context.Context, actor *plugin.Actor, action, _ string) bool {
	if actor == nil {
		return false
	}
	if action == plugin.ActionAdmin {
		return actor.HasRole("admin")
	}
	return true
}
