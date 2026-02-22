package common

import (
	"thirdcoast.systems/rewind/internal/db"
)

// DerefString safely dereferences a *string, returning "" if nil.
func DerefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// KeybindingsRowsToMap converts database keybinding rows to a map of action -> key.
// Nil rows and rows with empty action or key are skipped.
func KeybindingsRowsToMap(rows []*db.GetUserKeybindingsRow) map[string]string {
	result := make(map[string]string, len(rows))
	for _, row := range rows {
		if row == nil || row.Action == "" || row.Key == "" {
			continue
		}
		result[row.Action] = row.Key
	}
	return result
}
