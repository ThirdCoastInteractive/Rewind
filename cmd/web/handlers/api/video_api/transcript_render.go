package video_api

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/fileserver"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates/components"
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

		cues := parseVTT(string(data))

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
	if len(matches) > 0 {
		return matches[0]
	}
	return ""
}

// parseVTT parses a WebVTT file into TranscriptCue slices.
func parseVTT(text string) []components.TranscriptCue {
	lines := strings.Split(text, "\n")
	var cues []components.TranscriptCue
	i := 0

	for i < len(lines) {
		line := strings.TrimSpace(lines[i])
		i++

		if line == "" || strings.HasPrefix(line, "WEBVTT") || strings.HasPrefix(line, "NOTE") {
			continue
		}
		if !strings.Contains(line, "-->") {
			continue
		}

		parts := strings.SplitN(line, "-->", 2)
		if len(parts) != 2 {
			continue
		}
		startStr := strings.TrimSpace(strings.Fields(parts[0])[0])
		endFields := strings.Fields(strings.TrimSpace(parts[1]))
		if len(endFields) == 0 {
			continue
		}
		endStr := endFields[0]

		start := parseVTTTime(startStr)
		end := parseVTTTime(endStr)
		if start < 0 || end < 0 {
			continue
		}

		var textLines []string
		for i < len(lines) && strings.TrimSpace(lines[i]) != "" {
			textLines = append(textLines, strings.TrimSpace(lines[i]))
			i++
		}
		cueText := strings.Join(textLines, " ")
		if cueText != "" {
			cues = append(cues, components.TranscriptCue{
				Start: start,
				End:   end,
				Text:  cueText,
			})
		}
	}

	return cues
}

// parseVTTTime parses a VTT timestamp like "00:01:23.456" or "01:23.456".
func parseVTTTime(t string) float64 {
	t = strings.TrimSpace(t)
	// Handle HH:MM:SS.mmm or MM:SS.mmm
	parts := strings.Split(t, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return -1
	}

	var hh, mm float64
	var ssPart string

	if len(parts) == 3 {
		h, err := strconv.ParseFloat(parts[0], 64)
		if err != nil {
			return -1
		}
		hh = h
		m, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			return -1
		}
		mm = m
		ssPart = parts[2]
	} else {
		m, err := strconv.ParseFloat(parts[0], 64)
		if err != nil {
			return -1
		}
		mm = m
		ssPart = parts[1]
	}

	ss, err := strconv.ParseFloat(ssPart, 64)
	if err != nil {
		return -1
	}

	return hh*3600 + mm*60 + ss
}
