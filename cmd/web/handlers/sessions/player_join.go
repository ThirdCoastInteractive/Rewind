package sessions

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/internal/db"
)

func HandlePlayerJoin(dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		code := strings.TrimSpace(c.FormValue("session_code"))
		if len(code) != 6 {
			return c.Redirect(302, "/player")
		}

		// Verify session exists
		_, err := dbc.Queries(c.Request().Context()).GetPlayerSessionByCode(c.Request().Context(), code)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return c.Redirect(302, "/player")
			}
			return c.String(500, "Failed to join session")
		}

		return c.Redirect(302, "/player/"+code)
	}
}

// HandlePlayerSessionPage shows the player view for a session.
