package shownote_api

import (
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"

	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/internal/producer"
	"thirdcoast.systems/rewind/cmd/web/internal/scene"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

// renderPresetList patches the producer's saved scene presets into #live-presets.
func renderPresetList(c echo.Context, dbc *db.DatabaseConnection, userUUID pgtype.UUID, noteID string) error {
	presets, err := dbc.Queries(c.Request().Context()).ListPlayerScenePresetsByProducer(c.Request().Context(), userUUID)
	if err != nil {
		return echo.NewHTTPError(500, "failed to load presets")
	}
	sse := datastar.NewSSE(c.Response().Writer, c.Request())
	common.SetSSEHeaders(c)
	return sse.PatchElementTempl(templates.LiveScenePresetList(presets, noteID))
}

// HandleScenePresetsRender lists the producer's saved scene presets.
func HandleScenePresetsRender(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		noteUUID, userUUID, err := requireEditor(c, sm, dbc)
		if err != nil {
			return err
		}
		return renderPresetList(c, dbc, userUUID, noteUUID.String())
	}
}

// HandleSaveScenePreset saves the current scene (sent as the @post payload) as a
// named, reusable preset for this producer.
func HandleSaveScenePreset(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		var body struct {
			scene.Params
			Name string `json:"preset_name"`
		}
		_ = datastar.ReadSignals(c.Request(), &body)

		noteUUID, userUUID, err := requireEditor(c, sm, dbc)
		if err != nil {
			return err
		}
		name := strings.TrimSpace(body.Name)
		if name == "" {
			name = "Untitled"
		}
		sceneJSON := scene.Build(body.Params, time.Now().UnixMilli())
		if _, err := dbc.Queries(c.Request().Context()).UpsertPlayerScenePreset(c.Request().Context(), &db.UpsertPlayerScenePresetParams{
			ProducerID: userUUID,
			Name:       name,
			Scene:      sceneJSON,
		}); err != nil {
			return echo.NewHTTPError(500, "save failed")
		}
		return renderPresetList(c, dbc, userUUID, noteUUID.String())
	}
}

// HandleApplyScenePreset loads a saved preset, re-stamps its epoch, persists it as
// the live scene, and broadcasts it to viewers.
func HandleApplyScenePreset(sm *auth.SessionManager, dbc *db.DatabaseConnection, sceneHub *producer.SceneHub) echo.HandlerFunc {
	return func(c echo.Context) error {
		noteUUID, userUUID, err := requireEditor(c, sm, dbc)
		if err != nil {
			return err
		}
		presetUUID, err := common.RequireUUIDParam(c, "presetId")
		if err != nil {
			return err
		}
		ctx := c.Request().Context()
		preset, err := dbc.Queries(ctx).GetPlayerScenePresetByID(ctx, presetUUID)
		if err != nil || preset.ProducerID != userUUID {
			return echo.NewHTTPError(404, "preset not found")
		}
		sceneJSON := scene.Restamp(preset.Scene, time.Now().UnixMilli())
		if err := dbc.Queries(ctx).SetShowNoteScene(ctx, &db.SetShowNoteSceneParams{ID: noteUUID, SceneState: sceneJSON}); err != nil {
			return echo.NewHTTPError(500, "apply failed")
		}
		sceneHub.Broadcast(noteUUID.String(), sceneJSON)
		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		common.SetSSEHeaders(c)
		_ = sse
		return nil
	}
}

// HandleDeleteScenePreset removes a saved preset.
func HandleDeleteScenePreset(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		noteUUID, userUUID, err := requireEditor(c, sm, dbc)
		if err != nil {
			return err
		}
		presetUUID, err := common.RequireUUIDParam(c, "presetId")
		if err != nil {
			return err
		}
		_ = dbc.Queries(c.Request().Context()).DeletePlayerScenePreset(c.Request().Context(), &db.DeletePlayerScenePresetParams{
			ID:         presetUUID,
			ProducerID: userUUID,
		})
		return renderPresetList(c, dbc, userUUID, noteUUID.String())
	}
}
