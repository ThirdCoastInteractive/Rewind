package content

import (
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleVideosPage(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, username, err := sm.GetSession(c.Request())
		if err != nil {
			return c.Redirect(302, "/login")
		}

		// Render a fast shell; the videos grid is loaded asynchronously via Datastar SSE
		// from /api/videos/index (which also respects the current query string).
		return templates.Videos(nil, username).Render(c.Request().Context(), c.Response())
	}
}
