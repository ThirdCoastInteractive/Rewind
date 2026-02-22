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

// HandleHLS serves HLS files (master.m3u8, media playlists, and fMP4 segments)
// from /downloads/{video-id}/hls/.
// Route: GET /api/videos/:id/hls/*
func HandleHLS(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
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

		// Get the requested file path (everything after /hls/)
		requestedFile := c.Param("*")
		if requestedFile == "" {
			requestedFile = "master.m3u8"
		}

		// Sanitize: reject path traversal but allow subdirectory paths
		// (e.g. "streams/720p/video.m3u8" for multi-variant HLS)
		requestedFile = filepath.Clean(requestedFile)
		if strings.Contains(requestedFile, "..") {
			return c.String(400, "invalid path")
		}

		dir, err := fileserver.GetVideoDirForID(c.Request().Context(), videoID)
		if err != nil {
			return err
		}

		hlsPath := filepath.Join(dir, "hls", requestedFile)
		f, err := os.Open(hlsPath)
		if err != nil {
			if os.IsNotExist(err) {
				return c.String(404, "HLS file not found")
			}
			return c.String(500, "failed to read HLS file")
		}
		defer f.Close()

		// Set content type based on extension
		ext := strings.ToLower(filepath.Ext(requestedFile))
		switch ext {
		case ".m3u8":
			c.Response().Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		case ".m4s":
			c.Response().Header().Set("Content-Type", "video/iso.segment")
		default:
			c.Response().Header().Set("Content-Type", "application/octet-stream")
		}

		// Segments are immutable (VOD); cache aggressively.
		// Playlists can be cached too since they're static for VOD.
		if ext == ".m4s" {
			c.Response().Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			c.Response().Header().Set("Cache-Control", "public, max-age=3600")
		}

		c.Response().Header().Set("Accept-Ranges", "bytes")
		http.ServeContent(c.Response(), c.Request(), filepath.Base(hlsPath), time.Time{}, f)
		return nil
	}
}

// HandleHLSCheck checks whether HLS is available for a video.
// Route: GET /api/videos/:id/hls-ready
func HandleHLSCheck(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
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

		masterPath := filepath.Join(dir, "hls", "master.m3u8")
		_, statErr := os.Stat(masterPath)
		ready := statErr == nil

		return c.JSON(200, map[string]any{
			"hls_ready": ready,
		})
	}
}
