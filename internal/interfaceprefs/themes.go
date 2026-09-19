// Package interfaceprefs defines user-selectable appearance preferences.
package interfaceprefs

// Theme describes a supported palette independently of its light or dark mode.
type Theme struct{ ID, Name, Description string }

// Themes is the stable allowlist exposed by the appearance controls.
var Themes = []Theme{
	{"mono", "Mono", "Black, white, and nothing in between but gray."},
	{"federal", "Federal", "Midnight navy, archival cream, and heritage crimson."},
	{"amber", "Amber Terminal", "Phosphor amber and warm paper. After-hours control room."},
	{"ultraviolet", "Ultraviolet", "Electric violet with cyan highlights. A little excessive."},
}

// ValidTheme reports whether a palette can be selected.
func ValidTheme(value string) bool {
	for _, theme := range Themes {
		if theme.ID == value {
			return true
		}
	}
	return false
}

// ValidMode reports whether a color-scheme preference is supported.
func ValidMode(value string) bool { return value == "light" || value == "dark" || value == "system" }
