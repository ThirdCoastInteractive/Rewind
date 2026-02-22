// package video_api provides video-related API handlers.
package video_api

import (
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
)

// HandleClips returns clips for a video.
func HandleClips(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return c.String(401, "unauthorized")
		}

		videoUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}

		videoRow, err := dbc.Queries(c.Request().Context()).GetVideoByID(c.Request().Context(), videoUUID)
		if err != nil || videoRow == nil {
			return c.String(404, "video not found")
		}

		clips, err := dbc.Queries(c.Request().Context()).ListClipsByVideo(c.Request().Context(), videoUUID)
		if err != nil {
			return c.String(500, "failed to list clips")
		}

		return c.JSON(200, clips)
	}
}
