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

func HandlePreview(sm *auth.SessionManager, dbc *db.DatabaseConnection, fs *fileserver.FileServer) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return c.String(401, "unauthorized")
		}

		videoUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		videoID := videoUUID.String()
		dir, err := fileserver.GetVideoDirForID(c.Request().Context(), videoID)
		if err != nil {
			return err
		}
		preview := filepath.Join(dir, videoID+".preview.mp4")
		if _, err := os.Stat(preview); err != nil {
			return c.String(404, "preview not available")
		}
		return fs.ServeDiskFileWithCache(c, preview, "video/mp4", "private, max-age=86400, stale-while-revalidate=3600", fileserver.ETagWeakStat)
	}
}

// HandleSeekManifest serves the seek thumbnail manifest.
