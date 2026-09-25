package video_api

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/fileserver"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates/components"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/captions"
	"thirdcoast.systems/rewind/pkg/plugin"
)

// HandleTranscriptRender returns an SSE-patched, server-rendered transcript list.
// This replaces the former client-side TranscriptManager.render() which built
// HTML via createElement/innerHTML.
func HandleTranscriptRender(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return echo.NewHTTPError(401, "unauthorized")
		}

		videoUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		if _, err := common.RequireVideo(c, dbc.Queries(c.Request().Context()), videoUUID, plugin.ActionVideoRead); err != nil {
			return err
		}
		videoID := videoUUID.String()
		ctx := c.Request().Context()

		var cues []components.TranscriptCue
		if dir, err := fileserver.GetVideoDirForID(ctx, videoID); err == nil {
			if vttPath := findVTTFile(dir, videoID); vttPath != "" {
				if data, err := os.ReadFile(vttPath); err == nil {
					cues = transcriptCuesFromVTT(data)
				}
			}
		} else if b := plugin.Blobs(); b != nil {
			if data, ok := findVTTBlob(ctx, b, videoID); ok {
				cues = transcriptCuesFromVTT(data)
			}
		}
		if len(cues) == 0 {
			if tr, err := dbc.Queries(ctx).GetVideoTranscript(ctx, videoUUID); err == nil && tr != nil {
				if stored, err := captions.CuesFromStoredTranscript(tr.Cues, tr.Raw); err == nil {
					cues = make([]components.TranscriptCue, 0, len(stored))
					for _, cue := range stored {
						cues = append(cues, components.TranscriptCue{Start: cue.Start, End: cue.End, Text: cue.Text})
					}
				}
			}
		}

		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		sse.PatchElementTempl(components.TranscriptList(cues), datastar.WithSelectorID("transcript-list-inner"))
		return nil
	}
}

func transcriptCuesFromVTT(data []byte) []components.TranscriptCue {
	doc, err := captions.ParseString(string(data))
	if err != nil {
		return nil
	}
	lines := captions.ReadableLines(doc.Cues)
	cues := make([]components.TranscriptCue, 0, len(lines))
	for _, cue := range lines {
		cues = append(cues, components.TranscriptCue{Start: cue.Start, End: cue.End, Text: cue.Text})
	}
	return cues
}

// findVTTFile locates a VTT caption file for the given video, mirroring the
// captions handler logic (prefer English → und → any).
func findVTTFile(dir, videoID string) string {
	candidates := []string{
		filepath.Join(dir, videoID+".captions.en.vtt"),
		filepath.Join(dir, videoID+".captions.und.vtt"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	glob := filepath.Join(dir, videoID+".captions.*.vtt")
	matches, _ := filepath.Glob(glob)
	for _, p := range matches {
		if strings.HasSuffix(strings.ToLower(p), ".src.vtt") {
			continue
		}
		return p
	}
	return ""
}

func findVTTBlob(ctx context.Context, b plugin.Blob, videoID string) ([]byte, bool) {
	for _, name := range []string{videoID + ".captions.en.vtt", videoID + ".captions.und.vtt"} {
		if data, err := readBlobKey(ctx, b, plugin.VideoKey(videoID, name)); err == nil {
			return data, true
		}
	}
	names, err := b.List(ctx, videoID+"/")
	if err != nil {
		return nil, false
	}
	prefix := strings.ToLower(videoID) + ".captions."
	for _, key := range names {
		base := strings.ToLower(filepath.Base(key))
		if !strings.HasPrefix(base, prefix) || !strings.HasSuffix(base, ".vtt") || strings.HasSuffix(base, ".src.vtt") {
			continue
		}
		if data, err := readBlobKey(ctx, b, key); err == nil {
			return data, true
		}
	}
	return nil, false
}

func readBlobKey(ctx context.Context, b plugin.Blob, key string) ([]byte, error) {
	r, _, err := b.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}
