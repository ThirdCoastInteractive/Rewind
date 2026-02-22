// package video_api provides video-related API handlers.
package video_api

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/fileserver"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleThumbnail(sm *auth.SessionManager, dbc *db.DatabaseConnection, fs *fileserver.FileServer) echo.HandlerFunc {
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
		thumb := resolveThumbnailPath(dir, videoID, c.QueryParam("w"))
		if _, err := os.Stat(thumb); err == nil {
			return fs.ServeDiskFileWithCache(c, thumb, "image/jpeg", "private, max-age=86400, stale-while-revalidate=3600", fileserver.ETagStrongSHA256)
		}

		return c.String(404, "thumbnail not available")
	}
}

func resolveThumbnailPath(dir, videoID, rawWidth string) string {
	label := parseThumbnailLabel(rawWidth)
	if label != "" {
		labelPath := filepath.Join(dir, fmt.Sprintf("%s.thumbnail.%s.jpg", videoID, label))
		if _, err := os.Stat(labelPath); err == nil {
			return labelPath
		}
	}
	return filepath.Join(dir, videoID+".thumbnail.jpg")
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
