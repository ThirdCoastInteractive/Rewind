package settings_api

import (
	"log/slog"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/encryption"
)

func HandleSettingsInterface(sm *auth.SessionManager, dbc *db.DatabaseConnection, encMgr *encryption.Manager, sc *db.SettingsCache) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, username, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}

		// Interface preferences (sounds, motion) are stored in localStorage on client side
		// This handler just acknowledges the save and redirects back
		// Future: Could store in database if we want server-side preference sync

		slog.Info("interface preferences updated",
			"user", username,
			"sounds_enabled", c.FormValue("sounds_enabled"))

		// Get current cookies to redisplay settings page
		cookies, err := dbc.Queries(c.Request().Context()).GetUserCookies(c.Request().Context(), userUUID)
		if err != nil {
			slog.Error("failed to fetch cookies", "error", err)
		}
		cookiesValue := generateCookiesFile(encMgr, cookies)

		return renderSettingsPage(c, sm, dbc, encMgr, sc, userUUID, username, cookiesValue, "Interface preferences saved")
	}
}
