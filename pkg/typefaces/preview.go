package typefaces

import (
	"net/url"
	"strings"
)

// Popular is the empty-search list so the picker is not ABeeZee…Z.
var Popular = []CatalogItem{
	{ID: "inter", Family: "Inter", Category: "sans-serif"},
	{ID: "dm-sans", Family: "DM Sans", Category: "sans-serif"},
	{ID: "ibm-plex-sans", Family: "IBM Plex Sans", Category: "sans-serif"},
	{ID: "space-grotesk", Family: "Space Grotesk", Category: "sans-serif"},
	{ID: "outfit", Family: "Outfit", Category: "sans-serif"},
	{ID: "barlow", Family: "Barlow", Category: "sans-serif"},
	{ID: "oswald", Family: "Oswald", Category: "sans-serif"},
	{ID: "archivo-black", Family: "Archivo Black", Category: "sans-serif"},
	{ID: "tomorrow", Family: "Tomorrow", Category: "sans-serif"},
	{ID: "playfair-display", Family: "Playfair Display", Category: "serif"},
	{ID: "source-serif-4", Family: "Source Serif 4", Category: "serif"},
	{ID: "newsreader", Family: "Newsreader", Category: "serif"},
	{ID: "fraunces", Family: "Fraunces", Category: "serif"},
	{ID: "instrument-serif", Family: "Instrument Serif", Category: "serif"},
	{ID: "cinzel", Family: "Cinzel", Category: "serif"},
	{ID: "eb-garamond", Family: "EB Garamond", Category: "serif"},
	{ID: "libre-baskerville", Family: "Libre Baskerville", Category: "serif"},
	{ID: "cormorant-garamond", Family: "Cormorant Garamond", Category: "serif"},
	{ID: "spectral", Family: "Spectral", Category: "serif"},
	{ID: "bebas-neue", Family: "Bebas Neue", Category: "display"},
	{ID: "anton", Family: "Anton", Category: "display"},
	{ID: "orbitron", Family: "Orbitron", Category: "display"},
	{ID: "black-ops-one", Family: "Black Ops One", Category: "display"},
	{ID: "cinzel-decorative", Family: "Cinzel Decorative", Category: "display"},
	{ID: "unifrakturcook", Family: "UnifrakturCook", Category: "display"},
	{ID: "unifrakturmaguntia", Family: "UnifrakturMaguntia", Category: "display"},
	{ID: "pirata-one", Family: "Pirata One", Category: "display"},
	{ID: "medievalsharp", Family: "MedievalSharp", Category: "display"},
	{ID: "ibm-plex-mono", Family: "IBM Plex Mono", Category: "monospace"},
	{ID: "jetbrains-mono", Family: "JetBrains Mono", Category: "monospace"},
	{ID: "source-code-pro", Family: "Source Code Pro", Category: "monospace"},
	{ID: "space-mono", Family: "Space Mono", Category: "monospace"},
	{ID: "caveat", Family: "Caveat", Category: "handwriting"},
	{ID: "patrick-hand", Family: "Patrick Hand", Category: "handwriting"},
}

// Blackletter is the gothic/fraktur chip: chapter cards, not a Fontsource category.
var Blackletter = []CatalogItem{
	{ID: "unifrakturcook", Family: "UnifrakturCook", Category: "display"},
	{ID: "unifrakturmaguntia", Family: "UnifrakturMaguntia", Category: "display"},
	{ID: "pirata-one", Family: "Pirata One", Category: "display"},
	{ID: "medievalsharp", Family: "MedievalSharp", Category: "display"},
	{ID: "eater", Family: "Eater", Category: "display"},
	{ID: "new-rocker", Family: "New Rocker", Category: "display"},
	{ID: "metal-mania", Family: "Metal Mania", Category: "display"},
	{ID: "nosifer", Family: "Nosifer", Category: "display"},
}

var blackletterIDs = map[string]bool{
	"unifrakturcook":     true,
	"unifrakturmaguntia": true,
	"pirata-one":         true,
	"medievalsharp":      true,
	"eater":              true,
	"new-rocker":         true,
	"metal-mania":        true,
	"nosifer":            true,
}

// Pangram is the preview line in the picker (titles, not lorem).
const Pangram = "The Show · Chapter Four"

// NormalizeCategory maps chip ids to Fontsource categories.
func NormalizeCategory(cat string) string {
	switch strings.ToLower(strings.TrimSpace(cat)) {
	case "sans", "sans-serif", "sansserif":
		return "sans-serif"
	case "serif":
		return "serif"
	case "display":
		return "display"
	case "mono", "monospace":
		return "monospace"
	case "hand", "handwriting", "script":
		return "handwriting"
	case "blackletter", "gothic", "fraktur":
		return "blackletter"
	default:
		return strings.ToLower(strings.TrimSpace(cat))
	}
}

func itemMatchesCat(it CatalogItem, cat string) bool {
	if cat == "" {
		return true
	}
	if cat == "blackletter" {
		return blackletterIDs[it.ID] || blackletterIDs[slug(it.Family)]
	}
	return strings.EqualFold(it.Category, cat)
}

// GoogleCSSURL loads preview faces from Google Fonts CSS (not the TTF archive).
func GoogleCSSURL(families []string) string {
	seen := map[string]bool{}
	var q []string
	for _, f := range families {
		f = strings.TrimSpace(f)
		if f == "" || seen[strings.ToLower(f)] {
			continue
		}
		seen[strings.ToLower(f)] = true
		q = append(q, "family="+url.QueryEscape(f)+":wght@400;700")
	}
	if len(q) == 0 {
		return ""
	}
	return "https://fonts.googleapis.com/css2?" + strings.Join(q, "&") + "&display=swap"
}

// PreviewFamilies is bundled + installed + popular names for CSS preview.
func PreviewFamilies() []string {
	seen := map[string]bool{}
	var out []string
	add := func(f string) {
		k := strings.ToLower(f)
		if f == "" || seen[k] {
			return
		}
		seen[k] = true
		out = append(out, f)
	}
	for _, f := range Bundled() {
		add(f.Family)
	}
	for _, f := range Installed() {
		add(f.Family)
	}
	for _, f := range Popular {
		add(f.Family)
	}
	for _, f := range Blackletter {
		add(f.Family)
	}
	return out
}
