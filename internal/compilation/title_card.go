package compilation

import "strings"

// gothicTitleCard is the UnifrakturCook chapter card used on Ben Avery (and
// similar) stitch projects: cream text on black, 4 seconds.
func gothicTitleCard(text, subtitle string) map[string]any {
	card := map[string]any{
		"type":       "title",
		"text":       strings.TrimSpace(text),
		"duration":   4.0,
		"bg_color":   "#000000",
		"text_color": "#f5f0e6",
		"font":       "UnifrakturCook",
		"font_size":  56,
		"position":   "center",
	}
	if sub := strings.TrimSpace(subtitle); sub != "" {
		card["subtitle"] = sub
	}
	return card
}

func withGothicTitleCard(title, subtitle string, segments []map[string]any) []map[string]any {
	if strings.TrimSpace(title) == "" {
		return segments
	}
	for _, seg := range segments {
		kind, _ := seg["type"].(string)
		if kind == "title" {
			return segments
		}
	}
	out := make([]map[string]any, 0, len(segments)+1)
	out = append(out, gothicTitleCard(title, subtitle))
	return append(out, segments...)
}
