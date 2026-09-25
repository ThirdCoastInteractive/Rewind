// package video_api provides video-related API handlers.
package video_api

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/fileserver"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/plugin"
)

// HandleSeekManifest serves GET /videos/:id/seek/seek.json, returning the seek sprite sheet manifest.
func HandleSeekManifest(sm *auth.SessionManager, dbc *db.DatabaseConnection, fs *fileserver.FileServer) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return echo.NewHTTPError(401, "unauthorized")
		}
		videoUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		if _, err := common.RequireVideo(c, dbc.Queries(c.Request().Context()), videoUUID, plugin.ActionVideoRead); err != nil {
			return err
		}
		key := plugin.VideoKey(videoUUID.String(), "seek/seek.json")
		if err := fs.ServeKey(c, key, "application/json", "private, max-age=86400, stale-while-revalidate=3600", fileserver.ETagStrongSHA256); err != nil {
			if errors.Is(err, echo.ErrNotFound) {
				return c.JSON(http.StatusOK, map[string]any{
					"format": "rewind-seek-v1",
					"levels": []any{},
				})
			}
			return err
		}
		return nil
	}
}

// HandleSeekVTT serves the seek VTT file.
