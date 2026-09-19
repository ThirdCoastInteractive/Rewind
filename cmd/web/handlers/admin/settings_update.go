package admin

import (
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/runtimecfg"
)

// HandleAdminSettings saves instance access settings (registration and admin emails).
func HandleAdminSettings(sm *auth.SessionManager, dbc *db.DatabaseConnection, sc *db.SettingsCache) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		user, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return err
		}
		if err = runtimecfg.RequireAdmin(ctx, dbc, user); err != nil {
			return echo.NewHTTPError(403, "administrator access required")
		}
		failure := func(message string) error {
			return c.Redirect(302, "/admin/settings?err="+url.QueryEscape(message))
		}
		var emails []string
		for _, email := range strings.Split(c.FormValue("admin_emails"), ",") {
			if email = strings.TrimSpace(email); email != "" {
				emails = append(emails, email)
			}
		}
		if err = dbc.Queries(ctx).UpsertRegistrationEnabled(ctx, &db.UpsertRegistrationEnabledParams{RegistrationEnabled: c.FormValue("registration_enabled") != "", AdminEmails: emails}); err != nil {
			return failure("Failed to update instance settings")
		}
		if sc != nil {
			_ = sc.Reload(ctx)
		}
		return c.Redirect(302, "/admin/settings?msg="+url.QueryEscape("Access settings saved"))
	}
}
