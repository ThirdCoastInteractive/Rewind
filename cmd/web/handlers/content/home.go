package content

import (
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/templates"
)
// HandleHomePage serves GET /, rendering the main landing page.
func HandleHomePage(sm *auth.SessionManager) echo.HandlerFunc {
	return func(c echo.Context) error {
		var username string
		if _, u, err := sm.GetSession(c.Request()); err == nil {
			username = u
		}

		return templates.Index(username).Render(c.Request().Context(), c.Response())
	}
}
