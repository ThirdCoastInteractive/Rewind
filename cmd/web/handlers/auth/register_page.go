package auth

import (
	"log/slog"

	"github.com/labstack/echo/v4"
	webauth "thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleRegisterPage(sm *webauth.SessionManager, dbc *db.DatabaseConnection, sc *db.SettingsCache) echo.HandlerFunc {
	return func(c echo.Context) error {
		accessLevel, _ := c.Get("accessLevel").(string)
		if accessLevel != "" && accessLevel != string(webauth.AccessUnauthenticated) {
			return c.Redirect(302, "/")
		}

		q := dbc.Queries(c.Request().Context())
		userCount, err := q.CountUsers(c.Request().Context())
		if err != nil {
			slog.Error("failed to count users", "error", err)
			return templates.Register("An error occurred. Please try again.").Render(c.Request().Context(), c.Response())
		}

		if userCount > 0 {
			settings := sc.Get()
			if !settings.RegistrationEnabled {
				return templates.Register("Registration is disabled on this instance").Render(c.Request().Context(), c.Response())
			}
		}
		return templates.Register("").Render(c.Request().Context(), c.Response())
	}
}
