package content

import (
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/ctxkeys"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

// HandleFollowsPage serves GET /follows, the channel-following page (alias of /watches).
func HandleFollowsPage(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return HandleWatchesPage(sm, dbc)
}

// HandleWatchesPage serves GET /watches, rendering the follows page.
func HandleWatchesPage(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if live, _ := c.Request().Context().Value(ctxkeys.LiveProduct).(bool); live {
			return c.NoContent(http.StatusNotFound)
		}

		_, username, err := sm.GetSession(c.Request())
		if err != nil {
			return c.Redirect(302, "/login")
		}

		ctx := c.Request().Context()
		watches, err := dbc.Queries(ctx).ListWatchedChannels(ctx)
		if err != nil {
			slog.Error("failed to list watched channels", "error", err)
			watches = nil
		}

		// Channel pages link here with the add form pre-filled.
		prefillURL := c.QueryParam("url")
		prefillLabel := c.QueryParam("label")

		return templates.Watches(watches, prefillURL, prefillLabel, username).Render(ctx, c.Response())
	}
}
