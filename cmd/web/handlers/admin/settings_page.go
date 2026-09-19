package admin

import (
	"errors"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/runtimecfg"
)

// HandleAdminSettingsPage serves GET /admin/settings, the instance-wide configuration page.
func HandleAdminSettingsPage(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		user, username, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}
		if err = runtimecfg.RequireAdmin(c.Request().Context(), dbc, user); err != nil {
			return echo.NewHTTPError(403, "administrator access required")
		}
		ctx := c.Request().Context()
		settings, err := dbc.Queries(ctx).GetInstanceSettings(ctx)
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				slog.Error("failed to load instance settings", "error", err)
			}
			settings = &db.InstanceSetting{RegistrationEnabled: true, AdminEmails: []string{}}
		}
		if settings.AdminEmails == nil {
			settings.AdminEmails = []string{}
		}
		alertType, alertMsg := "", ""
		if errMsg := strings.TrimSpace(c.QueryParam("err")); errMsg != "" {
			alertType, alertMsg = "error", errMsg
		} else if msg := strings.TrimSpace(c.QueryParam("msg")); msg != "" {
			alertType, alertMsg = "success", msg
		}
		return templates.AdminSettings(username, settings.RegistrationEnabled, settings.AdminEmails, alertType, alertMsg).Render(ctx, c.Response())
	}
}

// HandleAdminSettings updates admin-level instance settings.
