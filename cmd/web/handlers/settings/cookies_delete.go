package settings_api

import (
	"log/slog"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/encryption"
)

func HandleSettingsDeleteCookies(sm *auth.SessionManager, dbc *db.DatabaseConnection, encMgr *encryption.Manager, sc *db.SettingsCache) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, username, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}

		if err := dbc.Queries(c.Request().Context()).DeleteUserCookies(c.Request().Context(), userUUID); err != nil {
			slog.Error("failed to clear cookies", "error", err)
			return renderSettingsPage(c, sm, dbc, encMgr, sc, userUUID, username, "", "Failed to delete cookies")
		}

		return renderSettingsPage(c, sm, dbc, encMgr, sc, userUUID, username, "", "Cookies cleared successfully")
	}
}

// HandleSettingsInterface handles interface preference updates.
