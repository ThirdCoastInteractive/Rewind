package admin

import (
	"log/slog"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleAdminUserEnable(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		q := dbc.Queries(c.Request().Context())

		targetUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return c.Redirect(302, "/admin/users?err="+url.QueryEscape("Invalid user id"))
		}

		requestedEnabled := strings.EqualFold(c.FormValue("enabled"), "true")

		target, err := q.SelectUserByID(c.Request().Context(), targetUUID)
		if err != nil {
			return c.Redirect(302, "/admin/users?err="+url.QueryEscape("User not found"))
		}
		if target.Role == db.UserRoleAdmin && !requestedEnabled {
			return c.Redirect(302, "/admin/users?err="+url.QueryEscape("Admins cannot be disabled"))
		}

		if err := q.SetUserEnabled(c.Request().Context(), &db.SetUserEnabledParams{ID: targetUUID, Enabled: requestedEnabled}); err != nil {
			slog.Error("failed to update user enabled", "error", err)
			return c.Redirect(302, "/admin/users?err="+url.QueryEscape("Failed to update user"))
		}

		// Invalidate the user's existing sessions so they are forced to re-login.
		if err := q.InvalidateUserSessions(c.Request().Context(), targetUUID); err != nil {
			slog.Error("failed to invalidate user sessions after enable change", "error", err)
		}

		return c.Redirect(302, "/admin/users?msg="+url.QueryEscape("User updated"))
	}
}

// HandleAdminUserRole changes a user's role.
