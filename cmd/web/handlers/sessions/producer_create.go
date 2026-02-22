package sessions

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleProducerCreateSession(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		accessLevel := c.Get("accessLevel").(string)
		if accessLevel == "unauthenticated" {
			return c.Redirect(302, "/login")
		}

		userUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}

		// Generate a 6-digit code
		code := fmt.Sprintf("%06d", time.Now().UnixNano()%1000000)
		expiresAt := time.Now().Add(4 * time.Hour)
		_, err = dbc.Queries(c.Request().Context()).CreatePlayerSession(c.Request().Context(), &db.CreatePlayerSessionParams{
			ProducerID:  userUUID,
			SessionCode: code,
			ExpiresAt:   pgtype.Timestamptz{Time: expiresAt, Valid: true},
		})
		if err != nil {
			slog.Error("failed to create player session", "error", err)
			return c.String(500, "Failed to create session")
		}

		return c.Redirect(302, "/producer/"+code)
	}
}

// HandleProducerSessionPage shows the producer control interface.
