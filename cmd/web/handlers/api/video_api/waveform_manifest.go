// package video_api provides video-related API handlers.
package video_api

import (
	"os"
	"path/filepath"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/fileserver"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
)

// HandleWaveformManifest serves the waveform manifest.
func HandleWaveformManifest(sm *auth.SessionManager, dbc *db.DatabaseConnection, fs *fileserver.FileServer) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return echo.NewHTTPError(401, "unauthorized")
		}
		videoUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		dir, err := fileserver.GetVideoDirForID(c.Request().Context(), videoUUID.String())
		if err != nil {
			return err
		}
		path := filepath.Join(dir, "waveform", "waveform.json")
		if _, err := os.Stat(path); err != nil {
			return c.String(404, "waveform not available")
		}
		return fs.ServeDiskFileWithCache(c, path, "application/json", "private, max-age=86400, stale-while-revalidate=3600", fileserver.ETagStrongSHA256)
	}
}
