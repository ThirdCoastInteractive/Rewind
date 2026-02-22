package settings_api

import (
	"log/slog"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/encryption"
)

func HandleSettingsDownloadCookies(sm *auth.SessionManager, dbc *db.DatabaseConnection, encMgr *encryption.Manager) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}

		cookies, err := dbc.Queries(c.Request().Context()).GetUserCookies(c.Request().Context(), userUUID)
		if err != nil {
			slog.Error("failed to fetch cookies", "error", err)
			return c.String(500, "Failed to load cookies")
		}
		cookiesContent := generateCookiesFile(encMgr, cookies)

		c.Response().Header().Set("Content-Disposition", "attachment; filename=cookies.txt")
		c.Response().Header().Set("Content-Type", "text/plain")
		return c.String(200, cookiesContent)
	}
}
