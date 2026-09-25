package plugin_test

import (
	"context"
	"testing"

	"thirdcoast.systems/rewind/pkg/plugin"
)

func TestAppearanceDefaultsAndValidation(t *testing.T) {
	defaults := plugin.Appearance(context.Background())
	if defaults.Theme != plugin.DefaultAppearanceTheme || defaults.ColorMode != plugin.DefaultAppearanceMode {
		t.Fatalf("defaults = %+v", defaults)
	}

	ctx := plugin.WithAppearance(context.Background(), "federal", "dark")
	appearance := plugin.Appearance(ctx)
	if appearance.Theme != "federal" || appearance.ColorMode != "dark" {
		t.Fatalf("appearance = %+v", appearance)
	}

	ctx = plugin.WithAppearance(ctx, "unknown", "auto")
	appearance = plugin.Appearance(ctx)
	if appearance.Theme != plugin.DefaultAppearanceTheme || appearance.ColorMode != plugin.DefaultAppearanceMode {
		t.Fatalf("invalid values = %+v", appearance)
	}
}

func TestAppearanceThemesReturnsCopy(t *testing.T) {
	themes := plugin.Themes()
	if len(themes) != 4 || themes[0].ID != "mono" {
		t.Fatalf("themes = %+v", themes)
	}
	themes[0].ID = "mutated"
	if plugin.Themes()[0].ID != "mono" {
		t.Fatal("theme catalog was mutated")
	}
}
