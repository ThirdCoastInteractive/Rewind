package admin

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleAdminUsersPage(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		username, _ := c.Get("currentUsername").(string)
		currentUserUUID, _ := c.Get("currentUserUUID").(pgtype.UUID)

		q := dbc.Queries(c.Request().Context())
		dbUsers, err := q.ListAllUsers(c.Request().Context())
		if err != nil {
			slog.Error("failed to list users", "error", err)
			return c.String(500, "failed to list users")
		}

		users := make([]templates.AdminUserRow, 0, len(dbUsers))
		for _, u := range dbUsers {
			users = append(users, templates.AdminUserRow{
				ID:       u.ID.String(),
				UserName: u.UserName,
				Email:    u.Email,
				Role:     string(u.Role),
				Enabled:  u.Enabled,
				IsSelf:   u.ID.String() == currentUserUUID.String(),
			})
		}

		alertType := ""
		alertMsg := ""
		if errMsg := c.QueryParam("err"); errMsg != "" {
			alertType = "error"
			alertMsg = errMsg
		} else if msg := c.QueryParam("msg"); msg != "" {
			alertType = "success"
			alertMsg = msg
		}

		return templates.AdminUsers(username, users, alertType, alertMsg).Render(c.Request().Context(), c.Response())
	}
}

// HandleAdminUserEnable enables or disables a user account.
