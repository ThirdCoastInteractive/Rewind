package content

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/ctxkeys"
	"thirdcoast.systems/rewind/cmd/web/templates"
)

// HandleHomePage serves GET /. RewindLive signed-in users land in the studio.
func HandleHomePage(sm *auth.SessionManager) echo.HandlerFunc {
	return func(c echo.Context) error {
		var username string
		if _, u, err := sm.GetSession(c.Request()); err == nil {
			username = u
		}
		if live, _ := c.Request().Context().Value(ctxkeys.LiveProduct).(bool); live && username != "" {
			return c.Redirect(http.StatusFound, "/live")
		}
		return templates.Index(username).Render(c.Request().Context(), c.Response())
	}
}
