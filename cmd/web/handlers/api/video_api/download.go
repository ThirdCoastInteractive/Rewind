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
	"thirdcoast.systems/rewind/pkg/plugin"
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

		videoData, err := common.RequireVideo(c, dbc.Queries(c.Request().Context()), videoUUID, plugin.ActionVideoRead)
		if err != nil {
			return err
		}

		videoID := videoUUID.String()
		b := plugin.Blobs()
		if b == nil {
			return c.String(404, "video file not available")
		}
		var key string
		for _, k := range plugin.MasterKeys(videoID) {
			if p, ok := b.LocalPath(k); ok {
				if _, err := os.Stat(p); err == nil {
					key = k
					break
				}
				continue
			}
			if r, _, err := b.Open(c.Request().Context(), k); err == nil {
				_ = r.Close()
				key = k
				break
			}
		}
		if key == "" {
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
		c.Response().Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s%s\"", safeTitle, filepath.Ext(key)))
		if err := fs.ServeKey(c, key, "application/octet-stream", "private, no-cache", fileserver.ETagWeakStat); err != nil {
			return c.String(404, "video file not available")
		}
		return nil
	}
}
