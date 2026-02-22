package sessions

import (
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
)

// HandleDeletePlayerSession returns a handler that deletes a player session owned by the current user.
func HandleDeletePlayerSession(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		accessLevel, _ := c.Get("accessLevel").(string)
		if accessLevel == "unauthenticated" {
			return c.String(401, "unauthorized")
		}

		userID, _, err := sm.GetSession(c.Request())
		if err != nil {
			return c.String(401, "unauthorized")
		}

		sessionUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}

		// Verify session belongs to user
		session, err := dbc.Queries(c.Request().Context()).GetPlayerSessionByID(c.Request().Context(), sessionUUID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return c.String(404, "session not found")
			}
			return c.String(500, "failed to get session")
		}

		if userID != session.ProducerID.String() {
			return c.String(403, "forbidden")
		}

		// Delete the session
		if err := dbc.Queries(c.Request().Context()).DeletePlayerSession(c.Request().Context(), sessionUUID); err != nil {
			slog.Error("failed to delete session", "error", err)
			return c.String(500, "failed to delete session")
		}

		return c.JSON(200, map[string]any{"status": "deleted"})
	}
}
