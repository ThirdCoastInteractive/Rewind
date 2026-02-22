package settings_api

import (
	"strings"

	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/templates"
)

// keybindingActions defines all available keybinding actions with their defaults.
var keybindingActions = []templates.KeybindingAction{
	{ID: "set_in_point", Label: "Set In Point", DefaultKey: "F14"},
	{ID: "set_out_point", Label: "Set Out Point", DefaultKey: "F15"},
	{ID: "create_clip", Label: "Create Clip", DefaultKey: "F16"},
	{ID: "play_pause", Label: "Play / Pause", DefaultKey: "F17"},
	{ID: "seek_back", Label: "Seek Back 10s", DefaultKey: "F18"},
	{ID: "seek_forward", Label: "Seek Forward 10s", DefaultKey: "F19"},
	{ID: "prev_frame", Label: "Previous Frame", DefaultKey: "F20"},
	{ID: "next_frame", Label: "Next Frame", DefaultKey: "F21"},
	{ID: "create_marker", Label: "Create Marker", DefaultKey: "F22"},
}

// keybindingSignals represents the DataStar signals for keybinding updates.
type keybindingSignals struct {
	Action string `json:"keybindingAction"`
	Key    string `json:"keybindingKey"`
}

// keybindingDefaultsMap returns a map of action IDs to their default keys.
func keybindingDefaultsMap() map[string]string {
	defaults := make(map[string]string, len(keybindingActions))
	for _, action := range keybindingActions {
		defaults[action.ID] = action.DefaultKey
	}
	return defaults
}

// buildKeybindingRowModels creates a list of keybinding row models from custom bindings.
func buildKeybindingRowModels(custom map[string]string) []templates.KeybindingRowModel {
	rows := make([]templates.KeybindingRowModel, 0, len(keybindingActions))
	for _, action := range keybindingActions {
		customKey, hasCustom := custom[action.ID]
		displayKey := action.DefaultKey
		if hasCustom && customKey != "" {
			displayKey = customKey
		}
		rows = append(rows, templates.KeybindingRowModel{
			ID:         action.ID,
			Label:      action.Label,
			DefaultKey: action.DefaultKey,
			DisplayKey: displayKey,
			HasCustom:  hasCustom && customKey != "" && customKey != action.DefaultKey,
		})
	}
	return rows
}

// findKeybindingRow finds a keybinding row by action ID.
func findKeybindingRow(rows []templates.KeybindingRowModel, action string) (templates.KeybindingRowModel, bool) {
	for _, row := range rows {
		if row.ID == action {
			return row, true
		}
	}
	return templates.KeybindingRowModel{}, false
}

// isValidKeybindingAction checks if an action ID is valid.
func isValidKeybindingAction(action string) bool {
	for _, item := range keybindingActions {
		if item.ID == action {
			return true
		}
	}
	return false
}

// isReservedKeybindingKey checks if a key is reserved and cannot be bound.
func isReservedKeybindingKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "shift", "control", "alt", "meta", "f13":
		return true
	default:
		return false
	}
}

// patchKeybindingError patches an error message into the keybinding UI.
func patchKeybindingError(sse *datastar.ServerSentEventGenerator, message string) {
	sse.PatchElementTempl(
		templates.KeybindingError(message),
		datastar.WithSelectorID("keybinding-error"),
	)
}
