package interfaceprefs

import "testing"

func TestThemeAllowlist(t *testing.T) {
	for _, theme := range Themes {
		if !ValidTheme(theme.ID) {
			t.Fatal(theme.ID)
		}
	}
	for _, value := range []string{"", "green", "../mono", "MONO", "<script>"} {
		if ValidTheme(value) {
			t.Fatalf("accepted %q", value)
		}
	}
	for _, value := range []string{"system", "light", "dark"} {
		if !ValidMode(value) {
			t.Fatal(value)
		}
	}
	if ValidMode("auto") {
		t.Fatal("accepted unknown mode")
	}
}
