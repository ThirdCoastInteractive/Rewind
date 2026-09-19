package templates

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"thirdcoast.systems/rewind/cmd/web/ctxkeys"
	"thirdcoast.systems/rewind/internal/runtimecfg"
)

func TestSettingsBooleanAttributes(t *testing.T) {
	var html bytes.Buffer
	if err := LiveSettings(runtimecfg.Defaults(), nil, "").Render(context.Background(), &html); err != nil {
		t.Fatal(err)
	}
	text := html.String()
	for _, invalid := range []string{`checked="false"`, `selected="false"`, `readonly="false"`} {
		if strings.Contains(text, invalid) {
			t.Fatalf("HTML boolean attribute incorrectly emitted: %s", invalid)
		}
	}
	if strings.Contains(text, `<details`) {
		t.Fatal("operational settings hidden in disclosures")
	}
	for _, group := range settingsGroups() {
		if !strings.Contains(text, `id="`+group.ID+`"`) {
			t.Fatalf("missing navigation target %s", group.ID)
		}
	}
}

func TestUserSettingsOmitInstanceConfig(t *testing.T) {
	var html bytes.Buffer
	if err := Settings("", "", true, "henry", "", nil).Render(context.Background(), &html); err != nil {
		t.Fatal(err)
	}
	text := html.String()
	for _, forbidden := range []string{`id="live-settings"`, `settings-agent`, `ADMIN SETTINGS`, `registration_enabled`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("user settings still includes instance config: %s", forbidden)
		}
	}
}

func TestInstanceSettingsIncludeLiveConfig(t *testing.T) {
	var html bytes.Buffer
	if err := AdminSettings("henry", true, nil, "", "").Render(context.Background(), &html); err != nil {
		t.Fatal(err)
	}
	text := html.String()
	for _, need := range []string{`id="live-settings"`, `settings-access`, `registration_enabled`, `/api/settings/live`} {
		if !strings.Contains(text, need) {
			t.Fatalf("instance page missing %s", need)
		}
	}
}

func TestAppearanceUsesStoredPreferences(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxkeys.InterfacePreferences, map[string]any{"theme": "federal", "color_mode": "light", "sounds_enabled": false})
	if appearancePreference(ctx, "theme") != "federal" || appearancePreference(ctx, "color_mode") != "light" || interfacePreference(ctx, "sounds_enabled") {
		t.Fatal("stored preferences lost")
	}
	if appearancePreference(context.Background(), "theme") != "mono" || appearancePreference(context.Background(), "color_mode") != "system" {
		t.Fatal("incorrect appearance defaults")
	}
	var html bytes.Buffer
	if err := AppearanceSettings().Render(ctx, &html); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html.String(), `checked="false"`) {
		t.Fatal("invalid radio boolean attribute")
	}
}
