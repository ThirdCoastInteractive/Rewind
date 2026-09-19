package shownote_api

import (
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"

	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

// HandleTakeDirector makes the calling host the director (single playback
// controller) for the show note, handing the role over from whoever held it.
func HandleTakeDirector(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		noteUUID, userUUID, err := requireEditor(c, sm, dbc)
		if err != nil {
			return err
		}
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)
		// Ensure the caller is tracked even if their WebRTC connection hasn't
		// registered yet, then make them the sole director.
		_, _ = q.UpsertProducerConnection(ctx, &db.UpsertProducerConnectionParams{ShowNoteID: noteUUID, UserID: userUUID})
		_ = q.ClearDirector(ctx, noteUUID)
		_ = q.SetDirector(ctx, &db.SetDirectorParams{ShowNoteID: noteUUID, UserID: userUUID})

		hosts, err := q.ListActiveConnections(ctx, noteUUID)
		if err != nil {
			return echo.NewHTTPError(500, "failed to load hosts")
		}
		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		common.SetSSEHeaders(c)
		return sse.PatchElementTempl(templates.LiveHostList(hosts, noteUUID.String()))
	}
}
