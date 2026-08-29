package video_api

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/fileserver"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates/components"
	"thirdcoast.systems/rewind/pkg/captions"
)

// HandleTranscriptRender returns an SSE-patched, server-rendered transcript list.
// This replaces the former client-side TranscriptManager.render() which built
// HTML via createElement/innerHTML.
func HandleTranscriptRender(sm *auth.SessionManager) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return echo.NewHTTPError(401, "unauthorized")
		}

		videoUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		videoID := videoUUID.String()

		dir, err := fileserver.GetVideoDirForID(c.Request().Context(), videoID)
		if err != nil {
			return nil
		}

		vttPath := findVTTFile(dir, videoID)
		if vttPath == "" {
			// No captions available – render empty state.
			sse := datastar.NewSSE(c.Response().Writer, c.Request())
			sse.PatchElementTempl(components.TranscriptList(nil), datastar.WithSelectorID("transcript-list-inner"))
			return nil
		}

		data, err := os.ReadFile(vttPath)
		if err != nil {
			return nil
		}

		doc, err := captions.ParseString(string(data))
		if err != nil {
			return nil
		}
		cues := make([]components.TranscriptCue, 0, len(doc.Cues))
		for _, cue := range doc.Cues {
			cues = append(cues, components.TranscriptCue{Start: cue.Start, End: cue.End, Text: cue.Text})
		}

		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		sse.PatchElementTempl(components.TranscriptList(cues), datastar.WithSelectorID("transcript-list-inner"))
		return nil
	}
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
