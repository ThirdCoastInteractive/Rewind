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

func HandleKeybindingUpdate(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()

		userUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return err
		}

		signals := &keybindingSignals{}
		if err := datastar.ReadSignals(c.Request(), signals); err != nil {
			return c.String(400, "invalid signals")
		}

		// IMPORTANT: NewSSE must be created AFTER ReadSignals.
		// NewSSE flushes response headers which closes the request body.
		sse := datastar.NewSSE(c.Response().Writer, c.Request())

		signals.Action = strings.TrimSpace(signals.Action)
		signals.Key = strings.TrimSpace(signals.Key)

		if !isValidKeybindingAction(signals.Action) {
			patchKeybindingError(sse, "Invalid action")
			return nil
		}
		if signals.Key == "" {
			patchKeybindingError(sse, "Invalid key")
			return nil
		}
		if strings.EqualFold(signals.Key, "F13") {
			patchKeybindingError(sse, "F13 is reserved")
			return nil
		}
		if isReservedKeybindingKey(signals.Key) {
			patchKeybindingError(sse, "That key cannot be bound")
			return nil
		}

		rows, _ := dbc.Queries(ctx).GetUserKeybindings(ctx, userUUID)
		effective := keybindingDefaultsMap()
		for _, row := range rows {
			if row == nil || row.Action == "" || row.Key == "" {
				continue
			}
			effective[row.Action] = row.Key
		}
		for action, key := range effective {
			if action == signals.Action {
				continue
			}
			if strings.EqualFold(key, signals.Key) {
				patchKeybindingError(sse, "That key is already in use")
				return nil
			}
		}

		if err := dbc.Queries(ctx).UpsertUserKeybinding(ctx, &db.UpsertUserKeybindingParams{
			UserID: userUUID,
			Action: signals.Action,
			Key:    signals.Key,
		}); err != nil {
			return c.String(500, "failed to save keybinding")
		}

		rows, _ = dbc.Queries(ctx).GetUserKeybindings(ctx, userUUID)
		bindings := common.KeybindingsRowsToMap(rows)
		rowModels := buildKeybindingRowModels(bindings)
		if rowModel, ok := findKeybindingRow(rowModels, signals.Action); ok {
			sse.PatchElementTempl(
				templates.KeybindingRow(rowModel),
				datastar.WithSelectorID("kb-"+signals.Action),
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

// HandleKeybindingDelete deletes a keybinding for the current user.
