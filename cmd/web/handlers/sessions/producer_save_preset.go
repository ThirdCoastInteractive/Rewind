package sessions

import (
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleProducerSaveScenePreset(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
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

		// Ensure the session belongs to this producer.
		session, err := dbc.Queries(c.Request().Context()).GetPlayerSessionByCode(c.Request().Context(), code)
		if err != nil {
			return c.Redirect(302, "/producer")
		}
		if session.ProducerID != userUUID {
			return c.Redirect(302, "/producer")
		}

		name := strings.TrimSpace(c.FormValue("preset_name"))
		if name == "" {
			return c.Redirect(302, "/producer/"+code)
		}

		scene := buildSceneFromForm(c)
		sceneJSON, err := json.Marshal(scene)
		if err != nil {
			return c.String(500, "Failed to serialize scene")
		}

		_, err = dbc.Queries(c.Request().Context()).UpsertPlayerScenePreset(c.Request().Context(), &db.UpsertPlayerScenePresetParams{
			ProducerID: userUUID,
			Name:       name,
			Scene:      sceneJSON,
		})
		if err != nil {
			slog.Error("failed to upsert scene preset", "error", err)
			return c.String(500, "Failed to save preset")
		}

		return c.Redirect(302, "/producer/"+code)
	}
}

// HandleProducerApplyScene applies scene changes to the session.
