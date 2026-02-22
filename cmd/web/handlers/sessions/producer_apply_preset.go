package sessions

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/internal/producer"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleProducerApplyScenePreset(sm *auth.SessionManager, dbc *db.DatabaseConnection, hub *producer.SceneHub) echo.HandlerFunc {
	return func(c echo.Context) error {
		accessLevel := c.Get("accessLevel").(string)
		if accessLevel == "unauthenticated" {
			return c.Redirect(302, "/login")
		}

		code := c.Param("code")
		if len(code) != 6 {
			return c.Redirect(302, "/producer")
		}

		userUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}

		presetID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return c.Redirect(302, "/producer/"+code)
		}

		ctx := c.Request().Context()
		session, err := dbc.Queries(ctx).GetPlayerSessionByCode(ctx, code)
		if err != nil {
			return c.Redirect(302, "/producer")
		}
		if session.ProducerID != userUUID {
			return c.Redirect(302, "/producer")
		}

		preset, err := dbc.Queries(ctx).GetPlayerScenePresetByID(ctx, presetID)
		if err != nil {
			return c.Redirect(302, "/producer/"+code)
		}
		if preset.ProducerID != userUUID {
			return c.Redirect(302, "/producer/"+code)
		}

		// Stamp a new epoch_ms so animations sync across all connected players
		var scene map[string]any
		if err := json.Unmarshal(preset.Scene, &scene); err != nil || scene == nil {
			scene = map[string]any{"version": 1}
		}
		bg, _ := scene["background"].(map[string]any)
		if bg == nil {
			bg = make(map[string]any)
			scene["background"] = bg
		}
		bg["epoch_ms"] = time.Now().UnixMilli()

		sceneJSON, err := json.Marshal(scene)
		if err != nil {
			return c.String(500, "Failed to serialize scene")
		}

		newState := setSceneInState(session.State, sceneJSON)
		if err := dbc.Queries(ctx).UpdatePlayerSessionState(ctx, &db.UpdatePlayerSessionStateParams{State: newState, ID: session.ID}); err != nil {
			slog.Error("failed to update player session scene", "error", err)
			return c.String(500, "Failed to apply preset")
		}

		hub.Broadcast(code, sceneJSON)
		return c.Redirect(302, "/producer/"+code)
	}
}

// HandleProducerDeleteScenePreset deletes a saved preset.
