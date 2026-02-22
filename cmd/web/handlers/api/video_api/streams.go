package video_api

import (
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
)

// HandleStreamFile serves a specific file from the video's streams/ directory.
// Route: GET /api/videos/:id/streams/:filename
func HandleStreamFile(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		// Auth: session cookie or remote player session code
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

		filename := c.Param("filename")
		if filename == "" {
			return c.String(400, "missing filename")
		}
		// Sanitize: only allow a flat filename, no path traversal
		filename = filepath.Base(filename)
		if strings.Contains(filename, "..") || filename == "." {
			return c.String(400, "invalid filename")
		}

		dir, err := fileserver.GetVideoDirForID(c.Request().Context(), videoID)
		if err != nil {
			return err
		}

		filePath := filepath.Join(dir, "streams", filename)
		f, err := os.Open(filePath)
		if err != nil {
			if os.IsNotExist(err) {
				return c.String(404, "stream file not found")
			}
			return c.String(500, "failed to read stream file")
		}
		defer f.Close()

		// Detect content type from extension
		ext := strings.ToLower(filepath.Ext(filename))
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

		http.ServeContent(c.Response(), c.Request(), filename, time.Time{}, f)
		return nil
	}
}
