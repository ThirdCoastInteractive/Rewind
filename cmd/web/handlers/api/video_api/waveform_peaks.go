// package video_api provides video-related API handlers.
package video_api

import (
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/fileserver"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/plugin"
)

// HandleWaveformPeaks serves the waveform peaks data.
func HandleWaveformPeaks(sm *auth.SessionManager, dbc *db.DatabaseConnection, fs *fileserver.FileServer) echo.HandlerFunc {
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
		key := plugin.VideoKey(videoUUID.String(), "waveform/peaks.i16")
		if err := fs.ServeKey(c, key, "application/octet-stream", "private, max-age=86400, stale-while-revalidate=3600", fileserver.ETagWeakStat); err != nil {
			return c.String(404, "waveform not available")
		}
		return nil
	}
}
