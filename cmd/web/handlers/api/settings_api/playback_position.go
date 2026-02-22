// package settings_api provides settings-related API handlers.
package settings_api

import (
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleSavePlaybackPosition(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return err
		}

		videoUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}

		var req struct {
			Position float64 `json:"position"`
		}
		if err := c.Bind(&req); err != nil {
			return c.String(400, "invalid json")
		}

		if req.Position < 0 {
			return c.String(400, "position must be >= 0")
		}

		err = dbc.Queries(c.Request().Context()).UpsertPlaybackPosition(c.Request().Context(), &db.UpsertPlaybackPositionParams{
			UserID:          userUUID,
			VideoID:         videoUUID,
			PositionSeconds: req.Position,
		})
		if err != nil {
			return c.String(500, "failed to save playback position")
		}

		return c.JSON(200, map[string]any{
			"success":  true,
			"position": req.Position,
		})
	}
}
