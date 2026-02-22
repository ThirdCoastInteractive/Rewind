package sessions

import (
	"encoding/base64"
	"log/slog"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleProducerSessionPage(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
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

		ctx := c.Request().Context()

		// Verify session exists and belongs to this producer.
		session, err := dbc.Queries(ctx).GetPlayerSessionByCode(ctx, code)
		if err != nil {
			return c.Redirect(302, "/producer")
		}
		if session.ProducerID != userUUID {
			return c.Redirect(302, "/producer")
		}

		presets, err := dbc.Queries(ctx).ListPlayerScenePresetsByProducer(ctx, userUUID)
		if err != nil {
			slog.Error("failed to list scene presets", "error", err)
			return c.String(500, "Failed to load scene presets")
		}

		presetInfos := make([]templates.ScenePresetInfo, 0, len(presets))
		for _, p := range presets {
			presetInfos = append(presetInfos, templates.ScenePresetInfo{
				ID:        p.ID.String(),
				Name:      p.Name,
				SceneB64:  base64.StdEncoding.EncodeToString(p.Scene),
				UpdatedAt: p.UpdatedAt.Time,
			})
		}

		currentSceneB64 := sceneToBase64(extractSceneFromState(session.State))

		username := ""
		if u, ok := c.Request().Context().Value("username").(string); ok {
			username = u
		}
		return templates.Producer(code, presetInfos, currentSceneB64, username).Render(c.Request().Context(), c.Response())
	}
}

// HandleProducerSaveScenePreset saves the current scene as a preset.
