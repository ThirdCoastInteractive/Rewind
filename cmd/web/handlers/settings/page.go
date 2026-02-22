package settings_api

import (
	"log/slog"
	"strings"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/encryption"
)

func HandleSettingsPage(sm *auth.SessionManager, dbc *db.DatabaseConnection, encMgr *encryption.Manager, sc *db.SettingsCache) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, username, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}

		cookies, err := dbc.Queries(c.Request().Context()).GetUserCookies(c.Request().Context(), userUUID)
		if err != nil {
			slog.Error("failed to fetch cookies", "error", err)
		}
		cookiesValue := generateCookiesFile(encMgr, cookies)

		msg := ""
		if errMsg := strings.TrimSpace(c.QueryParam("err")); errMsg != "" {
			msg = errMsg
		} else if okMsg := strings.TrimSpace(c.QueryParam("msg")); okMsg != "" {
			msg = okMsg
		}

		return renderSettingsPage(c, sm, dbc, encMgr, sc, userUUID, username, cookiesValue, msg)
	}
}
