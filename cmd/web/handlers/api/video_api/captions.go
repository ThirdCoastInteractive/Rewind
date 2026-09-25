// package video_api provides video-related API handlers.
package video_api

import (
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v4"
	xtlang "golang.org/x/text/language"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/fileserver"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/captions"
	"thirdcoast.systems/rewind/pkg/plugin"
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
		if _, err := common.RequireVideo(c, dbc.Queries(c.Request().Context()), videoUUID, plugin.ActionVideoRead); err != nil {
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
		cache := "private, max-age=86400, stale-while-revalidate=3600"
		for _, name := range []string{videoID + ".captions.en.vtt", videoID + ".captions.und.vtt"} {
			if err := fs.ServeKey(c, plugin.VideoKey(videoID, name), "text/vtt", cache, fileserver.ETagStrongSHA256); err == nil {
				return nil
			}
		}
		if b := plugin.Blobs(); b != nil {
			names, _ := b.List(c.Request().Context(), videoID+"/")
			for _, key := range names {
				base := strings.ToLower(filepath.Base(key))
				if !strings.HasPrefix(base, strings.ToLower(videoID)+".captions.") || !strings.HasSuffix(base, ".vtt") || strings.HasSuffix(base, ".src.vtt") {
					continue
				}
				if err := fs.ServeKey(c, key, "text/vtt", cache, fileserver.ETagStrongSHA256); err == nil {
					return nil
				}
			}
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
				name := videoID + ".captions." + lang + ".vtt"
				if dir, dirErr := fileserver.GetVideoDirForID(c.Request().Context(), videoID); dirErr == nil {
					dest := filepath.Join(dir, name)
					if writeErr := captions.WriteVTTFile(dest, cues); writeErr == nil {
						return fs.ServeKey(c, plugin.VideoKey(videoID, name), "text/vtt", cache, fileserver.ETagStrongSHA256)
					}
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
