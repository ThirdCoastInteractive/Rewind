package admin

import (
	"log/slog"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleAdminUserRole(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		q := dbc.Queries(c.Request().Context())
		currentUserUUID, _ := c.Get("currentUserUUID").(pgtype.UUID)

		targetUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return c.Redirect(302, "/admin/users?err="+url.QueryEscape("Invalid user id"))
		}

		requestedRole := strings.TrimSpace(c.FormValue("role"))
		if requestedRole != "admin" && requestedRole != "user" {
			return c.Redirect(302, "/admin/users?err="+url.QueryEscape("Invalid role"))
		}
		requestedRoleEnum := db.UserRole(requestedRole)

		target, err := q.SelectUserByID(c.Request().Context(), targetUUID)
		if err != nil {
			return c.Redirect(302, "/admin/users?err="+url.QueryEscape("User not found"))
		}

		if target.ID.String() == currentUserUUID.String() && requestedRoleEnum != db.UserRoleAdmin {
			return c.Redirect(302, "/admin/users?err="+url.QueryEscape("You cannot demote yourself"))
		}

		if target.Role == db.UserRoleAdmin && requestedRoleEnum != db.UserRoleAdmin {
			count, err := q.CountEnabledAdmins(c.Request().Context())
			if err != nil {
				slog.Error("failed to count enabled admins", "error", err)
				return c.Redirect(302, "/admin/users?err="+url.QueryEscape("Failed to update role"))
			}
			if count <= 1 {
				return c.Redirect(302, "/admin/users?err="+url.QueryEscape("You cannot demote the last enabled admin"))
			}
		}

		if err := q.SetUserRole(c.Request().Context(), &db.SetUserRoleParams{ID: targetUUID, Role: requestedRoleEnum}); err != nil {
			slog.Error("failed to update user role", "error", err)
			return c.Redirect(302, "/admin/users?err="+url.QueryEscape("Failed to update role"))
		}

		// Invalidate the user's existing sessions so they re-login with the new role.
		if err := q.InvalidateUserSessions(c.Request().Context(), targetUUID); err != nil {
			slog.Error("failed to invalidate user sessions after role change", "error", err)
		}

		return c.Redirect(302, "/admin/users?msg="+url.QueryEscape("User updated"))
	}
}

// HandleAdminRefreshAssets triggers bulk asset regeneration for all videos.
