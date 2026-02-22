// package video_api provides video-related API handlers.
package video_api

import (
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/fileserver"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleStream(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		// Check for session code (remote player auth)
		sessionCode := c.QueryParam("session")
		if sessionCode != "" && len(sessionCode) == 6 {
			if _, err := dbc.Queries(c.Request().Context()).GetPlayerSessionByCode(c.Request().Context(), sessionCode); err != nil {
				return c.String(401, "invalid session code")
			}
		} else {
			if _, _, err := sm.GetSession(c.Request()); err != nil {
				return c.String(401, "unauthorized")
			}
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

		// Find video file
		var videoPath string
		var f *os.File
		for _, ext := range VideoExtensions {
			p := filepath.Join(dir, videoID+".video"+ext)
			fh, openErr := os.Open(p)
			if openErr == nil {
				videoPath = p
				f = fh
				break
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

// HandleThumbnail serves the video thumbnail.
