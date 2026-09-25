// package video_api provides video-related API handlers.
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
		if dbc == nil {
			return c.String(404, "video not found")
		}
		row, err := common.RequireVideo(c, dbc.Queries(c.Request().Context()), videoUUID, plugin.ActionVideoRead)
		if err != nil || row == nil {
			return c.String(404, "video not found")
		}
		videoID := videoUUID.String()
		var stored string
		if row.VideoPath != nil {
			stored = *row.VideoPath
		}
		keys := streamKeys(videoID, stored)
		u, r, name, err := fileserver.OpenOrRedirect(c.Request().Context(), plugin.Blobs(), keys)
		if err != nil {
			return c.String(404, "video file not available")
		}
		if u != "" {
			return c.Redirect(http.StatusFound, u)
		}
		defer r.Close()
		ext := filepath.Ext(name)
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
		http.ServeContent(c.Response(), c.Request(), filepath.Base(name), time.Time{}, r)
		return nil
	}
}

func streamKeys(videoID, storedPath string) []string {
	var keys []string
	seen := map[string]bool{}
	add := func(k string) {
		if k == "" || seen[k] {
			return
		}
		seen[k] = true
		keys = append(keys, k)
	}
	add(blobKey(storedPath, videoID))
	if key := localStoredKey(storedPath, videoID); key != "" {
		add(key)
	}
	for _, k := range plugin.MasterKeys(videoID) {
		add(k)
	}
	return keys
}

// localStoredKey maps legacy absolute filesystem paths back to the current
// blob key when the local object really exists. Leading slash keys remain
// valid remote object keys and are therefore kept as the first candidate.
func localStoredKey(stored, videoID string) string {
	if strings.TrimSpace(stored) == "" || strings.TrimSpace(videoID) == "" {
		return ""
	}
	if !filepath.IsAbs(stored) && filepath.VolumeName(stored) == "" {
		return ""
	}
	base := filepath.Base(filepath.Clean(stored))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return ""
	}
	b := plugin.Blobs()
	if b == nil {
		return ""
	}
	key := plugin.VideoKey(videoID, base)
	p, ok := b.LocalPath(key)
	if !ok {
		return ""
	}
	if st, err := os.Stat(p); err != nil || !st.Mode().IsRegular() {
		return ""
	}
	return key
}

// blobKey turns a stored video_path into a Blob key.
func blobKey(stored, videoID string) string {
	s := strings.TrimSpace(stored)
	if s == "" {
		return ""
	}
	if vol := filepath.VolumeName(s); vol != "" {
		base := filepath.Base(s)
		if base != "" && videoID != "" {
			return plugin.VideoKey(videoID, base)
		}
		return ""
	}
	return s
}

// HandleThumbnail serves the video thumbnail.
