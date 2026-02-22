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

// HandleDownload serves the video file for download.
func HandleDownload(sm *auth.SessionManager, dbc *db.DatabaseConnection, fs *fileserver.FileServer) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return c.String(401, "unauthorized")
		}

		videoUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}

		videoData, err := dbc.Queries(c.Request().Context()).GetVideoWithDownloadJob(c.Request().Context(), videoUUID)
		if err != nil {
			return c.String(404, "video not found")
		}

		videoID := videoUUID.String()
		dir, _ := fileserver.GetVideoDirForID(c.Request().Context(), videoID)
		videoPath := ""
		for _, ext := range VideoExtensions {
			p := filepath.Join(dir, videoID+".video"+ext)
			if _, err := os.Stat(p); err == nil {
				videoPath = p
				break
			}
		}
		if strings.TrimSpace(videoPath) == "" {
			return c.String(404, "video file not available")
		}

		safeTitle := strings.Map(func(r rune) rune {
			switch r {
			case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
				return '-'
			default:
				return r
			}
		}, videoData.Title)
		if strings.TrimSpace(safeTitle) == "" {
			safeTitle = "video"
		}
		c.Response().Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s%s\"", safeTitle, filepath.Ext(videoPath)))
		return fs.ServeDiskFileWithCache(c, videoPath, "application/octet-stream", "private, no-cache", fileserver.ETagWeakStat)
	}
}
