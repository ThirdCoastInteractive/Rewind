package topics

import (
	"strings"

	"thirdcoast.systems/rewind/internal/wiki"
)

// extraAliases maps a canonical slug to extra alias norms that should point at it
// once that identity exists (usually from a wiki topic page).
var extraAliases = map[string][]string{
	"flock-alpr": {
		"flock-cameras", "flock-camera", "flock-surveillance", "flock-safety",
		"license-plate-readers", "license-plate-reader", "automated-license-plate-readers",
		"alprs", "vehicle-fingerprint", "vehicle-fingerprinting",
	},
}

type aliasSeed struct {
	Norm, Raw, Source string
}

func aliasesFor(slug, title string) []aliasSeed {
	out := []aliasSeed{
		{Normalize(slug), slug, "seed"},
		{Normalize(title), title, "wiki_title"},
	}
	for _, part := range splitTitle(title) {
		n := Normalize(part)
		if n != "" && n != slug {
			out = append(out, aliasSeed{n, part, "wiki_title"})
		}
	}
	for _, extra := range extraAliases[slug] {
		out = append(out, aliasSeed{extra, extra, "seed"})
	}
	seen := map[string]bool{}
	uniq := out[:0]
	for _, a := range out {
		if a.Norm == "" || seen[a.Norm] {
			continue
		}
		seen[a.Norm] = true
		uniq = append(uniq, a)
	}
	return uniq
}

func splitTitle(title string) []string {
	title = strings.ReplaceAll(title, " / ", "/")
	var parts []string
	for _, p := range strings.FieldsFunc(title, func(r rune) bool {
		return r == '/' || r == ':' || r == ',' || r == ';'
	}) {
		p = strings.TrimSpace(p)
		if p != "" {
			parts = append(parts, p)
		}
	}
	if s := wiki.SlugFromName(title); s != "" {
		parts = append(parts, s)
	}
	return parts
}
