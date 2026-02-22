package content

import (
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/templates"
)

func HandleHomePage(sm *auth.SessionManager) echo.HandlerFunc {
	return func(c echo.Context) error {
		var username string
		if _, u, err := sm.GetSession(c.Request()); err == nil {
			username = u
		}

		// Render a fast shell; recent videos are loaded asynchronously via Datastar SSE
		// from /api/videos/recent.
		return templates.Index(nil, username).Render(c.Request().Context(), c.Response())
	}
}
