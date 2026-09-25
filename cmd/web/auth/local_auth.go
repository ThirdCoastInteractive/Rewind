package auth

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/pkg/plugin"
)

// LocalAuth wraps cookie sessions. TenantID is empty on OSS.
type LocalAuth struct {
	Sessions *SessionManager
}

func NewLocalAuth(sessions *SessionManager) *LocalAuth {
	return &LocalAuth{Sessions: sessions}
}

func (a *LocalAuth) Current(r *http.Request) (*plugin.Actor, error) {
	uid, name, level, _, err := a.Sessions.ReadCookie(r)
	if err != nil {
		return nil, plugin.ErrNotAuthenticated
	}
	roles := []string{"user"}
	if level == AccessAdmin {
		roles = []string{"user", "admin"}
	}
	return &plugin.Actor{UserID: uid, Name: name, Roles: roles}, nil
}

func (a *LocalAuth) Login(http.ResponseWriter, *http.Request) error {
	return plugin.ErrNotSupported
}

func (a *LocalAuth) Logout(w http.ResponseWriter, r *http.Request) error {
	return a.Sessions.ClearSession(w, r)
}

func (a *LocalAuth) Register(http.ResponseWriter, *http.Request) error {
	return plugin.ErrNotSupported
}

func (a *LocalAuth) LoginPath() string { return "/login" }

func (a *LocalAuth) Mount(*echo.Echo) {}
