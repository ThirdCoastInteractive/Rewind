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
// HandlePreview serves GET /videos/:id/preview.mp4, returning the short hover-preview clip.
func HandlePreview(sm *auth.SessionManager, dbc *db.DatabaseConnection, fs *fileserver.FileServer) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return c.String(401, "unauthorized")
		}

		videoUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		if _, err := common.RequireVideo(c, dbc.Queries(c.Request().Context()), videoUUID, plugin.ActionVideoRead); err != nil {
			return err
		}
		videoID := videoUUID.String()
		key := plugin.VideoKey(videoID, videoID+".preview.mp4")
		if err := fs.ServeKey(c, key, "video/mp4", "private, max-age=86400, stale-while-revalidate=3600", fileserver.ETagWeakStat); err != nil {
			return c.String(404, "preview not available")
		}
		return nil
	}
}

// HandleSeekManifest serves the seek thumbnail manifest.
