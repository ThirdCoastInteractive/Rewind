// package video_api provides video-related API handlers.
package video_api

import (
	"fmt"
	"strings"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/fileserver"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/plugin"
)
// HandleThumbnail serves GET /videos/:id/thumbnail, returning the video thumbnail image at the requested size.
func HandleThumbnail(sm *auth.SessionManager, dbc *db.DatabaseConnection, fs *fileserver.FileServer) echo.HandlerFunc {
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
		cache := "private, max-age=86400, stale-while-revalidate=3600"
		if label := parseThumbnailLabel(c.QueryParam("w")); label != "" {
			key := plugin.VideoKey(videoID, fmt.Sprintf("%s.thumbnail.%s.jpg", videoID, label))
			if err := fs.ServeKey(c, key, "image/jpeg", cache, fileserver.ETagStrongSHA256); err == nil {
				return nil
			}
		}
		key := plugin.VideoKey(videoID, videoID+".thumbnail.jpg")
		if err := fs.ServeKey(c, key, "image/jpeg", cache, fileserver.ETagStrongSHA256); err == nil {
			return nil
		}
		return c.String(404, "thumbnail not available")
	}
}

func parseThumbnailLabel(raw string) string {
	label := strings.ToLower(strings.TrimSpace(raw))
	switch label {
	case "xs", "sm", "md", "lg", "xl", "2xl":
		return label
	default:
		return ""
	}
}

// HandlePreview serves the video preview.
