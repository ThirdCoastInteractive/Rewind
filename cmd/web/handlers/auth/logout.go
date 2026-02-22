package auth

import (
	"github.com/labstack/echo/v4"
	webauth "thirdcoast.systems/rewind/cmd/web/auth"
)

func HandleLogout(sm *webauth.SessionManager) echo.HandlerFunc {
	return func(c echo.Context) error {
		sm.ClearSession(c.Response().Writer, c.Request())
		return c.Redirect(302, "/login")
	}
}
