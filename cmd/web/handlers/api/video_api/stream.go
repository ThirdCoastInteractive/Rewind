// package video_api provides video-related API handlers.
package video_api

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/fileserver"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/plugin"
)

// HandleStream serves GET /videos/:id/stream, streaming the original video file with range-request support.
func HandleStream(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return c.String(401, "unauthorized")
		}

		videoUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		videoID := videoUUID.String()
		if b := plugin.Blobs(); b != nil {
			keys := []string{plugin.VideoKey(videoID, videoID+".video.mp4")}
			if row, err := dbc.Queries(c.Request().Context()).GetVideoByID(c.Request().Context(), videoUUID); err == nil && row != nil && row.VideoPath != nil {
				if k := blobKey(*row.VideoPath, videoID); k != "" {
					keys = append([]string{k}, keys...)
				}
			}
			for _, key := range keys {
				if u, err := b.PublicURL(c.Request().Context(), key, 15*time.Minute); err == nil && u != "" {
					return c.Redirect(http.StatusFound, u)
				}
			}
		}
		var videoPath string
		var f io.ReadSeekCloser
		if b := plugin.Blobs(); b != nil {
			for _, ext := range VideoExtensions {
				r, _, openErr := b.Open(c.Request().Context(), plugin.VideoKey(videoID, videoID+".video"+ext))
				if openErr == nil {
					videoPath = videoID + ".video" + ext
					f = r
					break
				}
			}
		}
		if f == nil {
			dir, err := fileserver.GetVideoDirForID(c.Request().Context(), videoID)
			if err != nil {
				return err
			}
			for _, ext := range VideoExtensions {
				p := filepath.Join(dir, videoID+".video"+ext)
				fh, openErr := os.Open(p)
				if openErr == nil {
					videoPath = p
					f = fh
					break
				}
			}
		}
		if f == nil {
			return c.String(404, "video file not available")
		}
		defer f.Close()

		// Detect content type
		ext := filepath.Ext(videoPath)
		contentType := "video/mp4"
		switch ext {
		case ".webm":
			contentType = "video/webm"
		case ".mkv":
			contentType = "video/x-matroska"
		}

		c.Response().Header().Set("Content-Type", contentType)
		c.Response().Header().Set("Cache-Control", "private, no-cache")
		c.Response().Header().Set("Accept-Ranges", "bytes")

		http.ServeContent(c.Response(), c.Request(), filepath.Base(videoPath), time.Time{}, f)
		return nil
	}
}

// blobKey turns a stored video_path into a Blob key when it isn't a filesystem path.
func blobKey(stored, videoID string) string {
	s := strings.TrimSpace(stored)
	if s == "" {
		return ""
	}
	if strings.Contains(s, string(filepath.Separator)) && (strings.HasPrefix(s, "/") || len(filepath.VolumeName(s)) > 0) {
		base := filepath.Base(s)
		if base != "" && videoID != "" {
			return plugin.VideoKey(videoID, base)
		}
		return ""
	}
	return strings.TrimPrefix(s, "/")
}

// HandleThumbnail serves the video thumbnail.
