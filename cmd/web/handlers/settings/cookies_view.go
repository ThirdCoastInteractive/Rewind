package settings_api

import (
	"log/slog"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleSettingsViewCookies(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, username, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}

		cookies, err := dbc.Queries(c.Request().Context()).GetUserCookies(c.Request().Context(), userUUID)
		if err != nil {
			slog.Error("failed to fetch cookies", "error", err)
			return c.String(500, "Failed to load cookies")
		}

		return templates.Cookies(cookies, username).Render(c.Request().Context(), c.Response())
	}
}
