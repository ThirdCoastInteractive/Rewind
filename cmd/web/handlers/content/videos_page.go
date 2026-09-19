package content

import (
	"github.com/labstack/echo/v4"
	"log/slog"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

// HandleVideosPage serves GET /videos, rendering the video library grid.
func HandleVideosPage(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, username, err := sm.GetSession(c.Request())
		if err != nil {
			return c.Redirect(302, "/login")
		}

		// Render a fast shell; the videos grid is loaded asynchronously via Datastar SSE
		// from /api/videos/index (which also respects the current query string).
		creators, listErr := dbc.Queries(c.Request().Context()).ListCreators(c.Request().Context())
		if listErr != nil {
			slog.Error("failed to list creators for video search", "error", listErr)
			creators = nil
		}
		return templates.Videos(nil, creators, c.QueryParam("q"), c.QueryParam("creator_id"), c.QueryParam("uploader"), username).Render(c.Request().Context(), c.Response())
	}
}
