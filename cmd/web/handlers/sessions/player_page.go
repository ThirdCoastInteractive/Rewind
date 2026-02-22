package sessions

import (
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/templates"
)

func HandlePlayerPage() echo.HandlerFunc {
	return func(c echo.Context) error {
		username := ""
		if u, ok := c.Request().Context().Value("username").(string); ok {
			username = u
		}
		return templates.RemotePlayer("", username).Render(c.Request().Context(), c.Response())
	}
}

// HandlePlayerJoin validates and joins a player session.
