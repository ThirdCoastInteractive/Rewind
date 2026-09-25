package plugin

import (
	"context"

	"thirdcoast.systems/rewind/internal/interfaceprefs"
)

const (
	// DefaultAppearanceTheme is the palette used when no valid preference exists.
	DefaultAppearanceTheme = "mono"
	// DefaultAppearanceMode is the color mode used when no valid preference exists.
	DefaultAppearanceMode = "system"
)

// AppearancePreferences are the validated appearance values for one request.
// Theme is a palette ID and ColorMode is light, dark, or system.
type AppearancePreferences struct {
	Theme     string
	ColorMode string
}

// Theme describes a palette supported by Rewind's appearance controls.
type Theme = interfaceprefs.Theme

type appearanceContextKey struct{}

var defaultAppearance = AppearancePreferences{
	Theme:     DefaultAppearanceTheme,
	ColorMode: DefaultAppearanceMode,
}

// WithAppearance stores validated appearance preferences in ctx. Empty and
// unknown values are replaced with the public defaults.
func WithAppearance(ctx context.Context, theme, mode string) context.Context {
	appearance := defaultAppearance
	if interfaceprefs.ValidTheme(theme) {
		appearance.Theme = theme
	}
	if interfaceprefs.ValidMode(mode) {
		appearance.ColorMode = mode
	}
	return context.WithValue(ctx, appearanceContextKey{}, appearance)
}

// Appearance returns the validated appearance preferences stored in ctx.
// Contexts without appearance values use the public defaults.
func Appearance(ctx context.Context) AppearancePreferences {
	if ctx != nil {
		if appearance, ok := ctx.Value(appearanceContextKey{}).(AppearancePreferences); ok {
			return appearance
		}
	}
	return defaultAppearance
}

// Themes returns a copy of the supported palette catalog.
func Themes() []Theme {
	themes := make([]Theme, len(interfaceprefs.Themes))
	copy(themes, interfaceprefs.Themes)
	return themes
}

// ValidTheme reports whether value is a supported palette ID.
func ValidTheme(value string) bool { return interfaceprefs.ValidTheme(value) }

// ValidColorMode reports whether value is a supported color mode.
func ValidColorMode(value string) bool { return interfaceprefs.ValidMode(value) }
