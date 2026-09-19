package shownote_api

import (
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"

	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/fileserver"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

// mp4 is the remux target; fall back to legacy containers.
var contentVideoExtensions = []string{".mp4", ".webm", ".mkv"}

// HandleContentStream serves a show note's referenced video to the live program
// output. Unauthenticated but scoped: the note must be live and the video must be
// referenced by one of its blocks (the note id is the access capability, reached
// via the public viewer page). This is how viewers get the program video without
// exposing the wider archive.
func HandleContentStream(dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		noteUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		videoUUID, err := common.RequireUUIDParam(c, "videoId")
		if err != nil {
			return err
		}
		ctx := c.Request().Context()
		note, err := dbc.Queries(ctx).GetShowNote(ctx, noteUUID)
		if err != nil || !note.IsLive {
			return c.String(404, "not available")
		}
		q := dbc.Queries(ctx)
		ok, err := q.NoteReferencesVideo(ctx, &db.NoteReferencesVideoParams{ShowNoteID: noteUUID, VideoID: videoUUID})
		if err == nil && !ok {
			ok, err = q.NoteWorkspaceReferencesVideo(ctx, &db.NoteWorkspaceReferencesVideoParams{ShowNoteID: noteUUID, VideoID: videoUUID})
		}
		if err != nil || !ok {
			return c.String(403, "not part of this show")
		}

		videoID := videoUUID.String()
		dir, err := fileserver.GetVideoDirForID(ctx, videoID)
		if err != nil {
			return c.String(404, "not found")
		}
		var path string
		var f *os.File
		for _, ext := range contentVideoExtensions {
			p := filepath.Join(dir, videoID+".video"+ext)
			if fh, openErr := os.Open(p); openErr == nil {
				path, f = p, fh
				break
			}
		}
		if f == nil {
			return c.String(404, "video file not available")
		}
		defer f.Close()

		ct := "video/mp4"
		switch filepath.Ext(path) {
		case ".webm":
			ct = "video/webm"
		case ".mkv":
			ct = "video/x-matroska"
		}
		c.Response().Header().Set("Content-Type", ct)
		c.Response().Header().Set("Cache-Control", "private, no-cache")
		c.Response().Header().Set("Accept-Ranges", "bytes")
		http.ServeContent(c.Response(), c.Request(), filepath.Base(path), time.Time{}, f)
		return nil
	}
}

// HandleContentSourcesRender lists the show note's video blocks as playable
// program content for the producer.
func HandleContentSourcesRender(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		noteUUID, _, err := requireEditor(c, sm, dbc)
		if err != nil {
			return err
		}
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)
		refs, refErr := q.ListPlayableShowNoteReferences(ctx, noteUUID)
		if refErr == nil && len(refs) > 0 {
			var sources []templates.LiveContentSource
			for _, ref := range refs {
				title := ref.Label
				if title == "" {
					title = "(untitled)"
				}
				sources = append(sources, templates.LiveContentSource{
					OccurrenceKey: ref.OccurrenceKey, VideoID: ref.VideoID.String(), Title: title,
					Kind: ref.Kind, StartSeconds: ref.StartSeconds, EndSeconds: ref.EndSeconds,
				})
			}
			sse := datastar.NewSSE(c.Response().Writer, c.Request())
			common.SetSSEHeaders(c)
			return sse.PatchElementTempl(templates.LiveContentSourceList(sources, noteUUID.String()))
		}
		blocks, err := q.ListBlocksForShowNote(ctx, noteUUID)
		if err != nil {
			return echo.NewHTTPError(500, "failed to load blocks")
		}
		var sources []templates.LiveContentSource
		for _, b := range blocks {
			if b.BlockType == "video" && b.VideoID.Valid {
				title := b.Title
				if title == "" {
					title = "(untitled)"
				}
				sources = append(sources, templates.LiveContentSource{OccurrenceKey: b.ID.String(), VideoID: b.VideoID.String(), Title: title, Kind: b.BlockType})
			}
		}
		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		common.SetSSEHeaders(c)
		return sse.PatchElementTempl(templates.LiveContentSourceList(sources, noteUUID.String()))
	}
}
