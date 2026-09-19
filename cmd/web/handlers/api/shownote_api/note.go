package shownote_api

import (
	"encoding/json"
	"log/slog"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"

	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
)

// HandleUpdateShowNote edits the note's title/description/live flag. Returns 204
// (no patch — title/live are reflected by the editing client's own signals).
func HandleUpdateShowNote(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		noteUUID, _, err := requireEditor(c, sm, dbc)
		if err != nil {
			return err
		}

		var req struct {
			Title       *string `json:"title"`
			Description *string `json:"description"`
			IsLive      *bool   `json:"is_live"`
		}
		if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
			return echo.NewHTTPError(400, "invalid body")
		}

		ctx := c.Request().Context()
		if _, err := dbc.Queries(ctx).UpdateShowNote(ctx, &db.UpdateShowNoteParams{
			ID:          noteUUID,
			Title:       req.Title,
			Description: req.Description,
			IsLive:      req.IsLive,
		}); err != nil {
			slog.Error("show note: update failed", "error", err)
			return echo.NewHTTPError(500, "update failed")
		}
		return c.NoContent(204)
	}
}

// HandleDeleteShowNote deletes the note (owner only) and redirects the client
// back to the library via an SSE-executed script.
func HandleDeleteShowNote(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.String(401, "unauthorized")
		}
		noteUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		ctx := c.Request().Context()
		if err := dbc.Queries(ctx).DeleteShowNote(ctx, &db.DeleteShowNoteParams{ID: noteUUID, OwnerID: userUUID}); err != nil {
			slog.Error("show note: delete failed", "error", err)
			return echo.NewHTTPError(500, "delete failed")
		}
		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		common.SetSSEHeaders(c)
		return sse.ExecuteScript("window.location.href = '/show-notes'")
	}
}
