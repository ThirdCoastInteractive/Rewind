package video_api

import (
	"net/http"
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

// HandleStreamFile serves a specific file from the video's streams/ directory.
// Route: GET /api/videos/:id/streams/:filename
func HandleStreamFile(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
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

		filename := c.Param("filename")
		if filename == "" {
			return c.String(400, "missing filename")
		}
		// Sanitize: only allow a flat filename, no path traversal
		filename = filepath.Base(filename)
		if strings.Contains(filename, "..") || filename == "." {
			return c.String(400, "invalid filename")
		}

		ext := strings.ToLower(filepath.Ext(filename))
		contentType := "video/mp4"
		switch ext {
		case ".webm":
			contentType = "video/webm"
		case ".mkv":
			contentType = "video/x-matroska"
		}
		key := plugin.VideoKey(videoID, "streams/"+filename)
		b := plugin.Blobs()
		u, r, _, err := fileserver.OpenOrRedirect(c.Request().Context(), b, []string{key})
		if err != nil {
			return c.String(404, "stream file not found")
		}
		if u != "" {
			return c.Redirect(http.StatusFound, u)
		}
		defer r.Close()
		c.Response().Header().Set("Content-Type", contentType)
		c.Response().Header().Set("Cache-Control", "private, no-cache")
		c.Response().Header().Set("Accept-Ranges", "bytes")
		http.ServeContent(c.Response(), c.Request(), filename, time.Time{}, r)
		return nil
	}
}
