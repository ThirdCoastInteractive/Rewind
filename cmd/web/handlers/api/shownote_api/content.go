package shownote_api

import (
	"net/http"
	"path/filepath"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"

	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/fileserver"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/plugin"
)

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
		q := dbc.Queries(ctx)
		// Public program output is capability-based and must behave identically
		// for anonymous and logged-in viewers. The live note UUID is the
		// capability; private editor routes use tenant guards elsewhere.
		note, err := q.GetShowNote(ctx, noteUUID)
		if err != nil || !note.IsLive {
			return c.String(404, "not available")
		}
		video, err := q.GetVideoByID(ctx, videoUUID)
		if err != nil || (plugin.LiveIngest() != nil && (!note.TenantID.Valid || note.TenantID.Bytes == [16]byte{} || note.TenantID != video.TenantID)) {
			return c.String(404, "video not available")
		}
		ok, err := q.NoteReferencesVideo(ctx, &db.NoteReferencesVideoParams{ShowNoteID: noteUUID, VideoID: videoUUID})
		if err == nil && !ok {
			ok, err = q.NoteWorkspaceReferencesVideo(ctx, &db.NoteWorkspaceReferencesVideoParams{ShowNoteID: noteUUID, VideoID: videoUUID})
		}
		if err != nil || !ok {
			return c.String(403, "not part of this show")
		}

		videoID := videoUUID.String()
		u, r, name, err := fileserver.OpenOrRedirect(ctx, plugin.Blobs(), plugin.MasterKeys(videoID))
		if err != nil {
			return c.String(404, "video file not available")
		}
		if u != "" {
			return c.Redirect(http.StatusFound, u)
		}
		defer r.Close()
		ct := "video/mp4"
		switch filepath.Ext(name) {
		case ".webm":
			ct = "video/webm"
		case ".mkv":
			ct = "video/x-matroska"
		}
		c.Response().Header().Set("Content-Type", ct)
		c.Response().Header().Set("Cache-Control", "private, no-cache")
		c.Response().Header().Set("Accept-Ranges", "bytes")
		http.ServeContent(c.Response(), c.Request(), filepath.Base(name), time.Time{}, r)
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
