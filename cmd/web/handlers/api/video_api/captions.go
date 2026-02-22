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

// HandleCaptions serves the video captions.
func HandleCaptions(sm *auth.SessionManager, dbc *db.DatabaseConnection, fs *fileserver.FileServer) echo.HandlerFunc {
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

		// Prefer English, then und, then any captions.*.vtt.
		candidates := []string{
			filepath.Join(dir, videoID+".captions.en.vtt"),
			filepath.Join(dir, videoID+".captions.und.vtt"),
		}
		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				return fs.ServeDiskFileWithCache(c, p, "text/vtt", "private, max-age=86400, stale-while-revalidate=3600", fileserver.ETagStrongSHA256)
			}
		}
		glob := filepath.Join(dir, videoID+".captions.*.vtt")
		matches, _ := filepath.Glob(glob)
		if len(matches) == 0 {
			return c.String(404, "captions not available")
		}
		return fs.ServeDiskFileWithCache(c, matches[0], "text/vtt", "private, max-age=86400, stale-while-revalidate=3600", fileserver.ETagStrongSHA256)
	}
}
