package auth

import (
	"errors"

	"github.com/labstack/echo/v4"
	webauth "thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/pkg/plugin"
)

// HandleLogout serves GET /logout, clearing the session cookie and redirecting to the login page.
func HandleLogout(sm *webauth.SessionManager) echo.HandlerFunc {
	return func(c echo.Context) error {
		if a := plugin.Auth(); a != nil {
			// Plugin success clears the cookie; ErrNotSupported (or any error) falls back.
			if err := a.Logout(c.Response().Writer, c.Request()); errors.Is(err, plugin.ErrNotSupported) || err != nil {
				sm.ClearSession(c.Response().Writer, c.Request())
			}
		} else {
			sm.ClearSession(c.Response().Writer, c.Request())
		}
		return c.Redirect(302, "/login")
	}
}
