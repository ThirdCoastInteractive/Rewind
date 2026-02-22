// package settings_api provides settings-related API handlers.
package settings_api

import (
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleKeybindingDelete(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		sse := datastar.NewSSE(c.Response().Writer, c.Request())

		userUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return err
		}

		action := strings.TrimSpace(c.Param("action"))
		if !isValidKeybindingAction(action) {
			return c.String(400, "invalid action")
		}

		if err := dbc.Queries(ctx).DeleteUserKeybinding(ctx, &db.DeleteUserKeybindingParams{
			UserID: userUUID,
			Action: action,
		}); err != nil {
			return c.String(500, "failed to delete keybinding")
		}

		rows, _ := dbc.Queries(ctx).GetUserKeybindings(ctx, userUUID)
		bindings := common.KeybindingsRowsToMap(rows)
		rowModels := buildKeybindingRowModels(bindings)
		if rowModel, ok := findKeybindingRow(rowModels, action); ok {
			sse.PatchElementTempl(
				templates.KeybindingRow(rowModel),
				datastar.WithSelectorID("kb-"+action),
			)
		}

		sse.PatchElementTempl(
			templates.KeybindingsData(bindings),
			datastar.WithSelectorID("rewind-keybindings"),
		)

		patchKeybindingError(sse, "")

		return nil
	}
}

// HandleKeybindingReset resets all keybindings to defaults for the current user.
