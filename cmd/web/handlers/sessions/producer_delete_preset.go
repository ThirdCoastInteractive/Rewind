package sessions

import (
	"log/slog"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleProducerDeleteScenePreset(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		accessLevel := c.Get("accessLevel").(string)
		if accessLevel == "unauthenticated" {
			return c.Redirect(302, "/login")
		}

		code := c.Param("code")
		if len(code) != 6 {
			return c.Redirect(302, "/producer")
		}

		userUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}

		presetID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return c.Redirect(302, "/producer/"+code)
		}

		// Ensure session belongs to this producer
		session, err := dbc.Queries(c.Request().Context()).GetPlayerSessionByCode(c.Request().Context(), code)
		if err != nil {
			return c.Redirect(302, "/producer")
		}
		if session.ProducerID != userUUID {
			return c.Redirect(302, "/producer")
		}

		if err := dbc.Queries(c.Request().Context()).DeletePlayerScenePreset(c.Request().Context(), &db.DeletePlayerScenePresetParams{ID: presetID, ProducerID: userUUID}); err != nil {
			slog.Error("failed to delete scene preset", "error", err)
			return c.String(500, "Failed to delete preset")
		}

		return c.Redirect(302, "/producer/"+code)
	}
}

// HandlePlayerPage shows the player join page.
