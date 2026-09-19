// package video_api provides video-related API handlers.
package video_api

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v4"
	xtlang "golang.org/x/text/language"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/fileserver"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/captions"
)

// HandleCaptions serves the video captions.
func HandleCaptions(sm *auth.SessionManager, dbc *db.DatabaseConnection, fs *fileserver.FileServer) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return c.String(401, "unauthorized")
		}

		videoUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		videoID := videoUUID.String()
		// Explicit audio repairs are authoritative; retain original sidecars as
		// archive evidence instead of serving their corrupted cues again.
		if repaired, err := dbc.Queries(c.Request().Context()).GetRepairedVideoTranscript(c.Request().Context(), videoUUID); err == nil {
			cues, err := captions.CuesFromStoredTranscript(repaired.Cues, repaired.Raw)
			if err != nil {
				return err
			}
			var body strings.Builder
			if err := captions.WriteVTT(&body, cues); err != nil {
				return err
			}
			c.Response().Header().Set("Cache-Control", "private, no-store")
			return c.Blob(200, "text/vtt; charset=utf-8", []byte(body.String()))
		}
		dir, err := fileserver.GetVideoDirForID(c.Request().Context(), videoID)
		if err != nil {
			return err
		}

		// Prefer English, then und, then any captions.*.vtt.
		candidates := []string{
			filepath.Join(dir, videoID+".captions.en.vtt"),
			filepath.Join(dir, videoID+".captions.und.vtt"),
		}
		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				return fs.ServeDiskFileWithCache(c, p, "text/vtt", "private, max-age=86400, stale-while-revalidate=3600", fileserver.ETagStrongSHA256)
			}
		}
		glob := filepath.Join(dir, videoID+".captions.*.vtt")
		matches, _ := filepath.Glob(glob)
		for _, p := range matches {
			if strings.HasSuffix(strings.ToLower(p), ".src.vtt") {
				continue
			}
			return fs.ServeDiskFileWithCache(c, p, "text/vtt", "private, max-age=86400, stale-while-revalidate=3600", fileserver.ETagStrongSHA256)
		}

		// Some transcript engines persist timed cues directly without writing a
		// sidecar. Materialize the exact file contract the player expects instead
		// of re-running transcription (the model/quant is deliberately unchanged).
		transcript, transcriptErr := dbc.Queries(c.Request().Context()).GetVideoTranscript(c.Request().Context(), videoUUID)
		if transcriptErr == nil && transcript != nil {
			cues, cuesErr := captions.CuesFromStoredTranscript(transcript.Cues, transcript.Raw)
			if cuesErr == nil {
				lang := xtlang.Tag(transcript.Lang).String()
				if lang == "" {
					lang = "und"
				}
				dest := filepath.Join(dir, videoID+".captions."+lang+".vtt")
				if writeErr := captions.WriteVTTFile(dest, cues); writeErr == nil {
					return fs.ServeDiskFileWithCache(c, dest, "text/vtt", "private, max-age=86400, stale-while-revalidate=3600", fileserver.ETagStrongSHA256)
				}
				// A read-only mount should not make subtitles disappear. Serve the
				// generated VTT directly even when persisting the repair failed.
				var body strings.Builder
				if writeErr := captions.WriteVTT(&body, cues); writeErr == nil {
					c.Response().Header().Set("Cache-Control", "private, max-age=300")
					return c.Blob(200, "text/vtt; charset=utf-8", []byte(body.String()))
				}
			}
		}
		return c.String(404, "captions not available")
	}
}
