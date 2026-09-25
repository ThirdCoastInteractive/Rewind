// package video_api provides video-related API handlers.
package video_api

import (
	"strings"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/fileserver"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/plugin"
)
// HandleSeekVTT serves GET /videos/:id/seek/levels/:level/seek.vtt, returning the WebVTT cue file for seek thumbnails.
func HandleSeekVTT(sm *auth.SessionManager, dbc *db.DatabaseConnection, fs *fileserver.FileServer) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return echo.NewHTTPError(401, "unauthorized")
		}
		level := strings.TrimSpace(c.Param("level"))
		if !ReSeekLevelParam.MatchString(level) {
			return c.String(400, "invalid level")
		}
		videoUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		if _, err := common.RequireVideo(c, dbc.Queries(c.Request().Context()), videoUUID, plugin.ActionVideoRead); err != nil {
			return err
		}
		key := plugin.VideoKey(videoUUID.String(), "seek/levels/"+level+"/seek.vtt")
		if err := fs.ServeKey(c, key, "text/vtt", "private, max-age=86400, stale-while-revalidate=3600", fileserver.ETagStrongSHA256); err != nil {
			return c.String(404, "seek thumbnails not available")
		}
		return nil
	}
}

// HandleSeekSheet serves a seek thumbnail sheet.
