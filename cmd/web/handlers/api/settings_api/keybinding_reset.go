// package settings_api provides settings-related API handlers.
package settings_api

import (
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleKeybindingReset(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		sse := datastar.NewSSE(c.Response().Writer, c.Request())

		userUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return err
		}

		if err := dbc.Queries(ctx).ResetUserKeybindings(ctx, userUUID); err != nil {
			return c.String(500, "failed to reset keybindings")
		}

		bindings := map[string]string{}
		rowModels := buildKeybindingRowModels(bindings)

		sse.PatchElementTempl(
			templates.KeybindingSettingsContent(rowModels),
			datastar.WithSelectorID("keybinding-settings"),
		)

		sse.PatchElementTempl(
			templates.KeybindingsData(bindings),
			datastar.WithSelectorID("rewind-keybindings"),
		)

		patchKeybindingError(sse, "")

		return nil
	}
}

// HandleSavePlaybackPosition saves the user's current playback position for a video.
