// Package topics canonicalizes context-window labels onto durable subject identities.
package topics

import (
	"strings"
	"unicode"

	"thirdcoast.systems/rewind/internal/wiki"
)

// Bind is one window → topic identity resolved from a raw label.
type Bind struct {
	Slug      string
	Title     string
	Raw       string
	MatchKind string // topic_label | entity | title | alias
	NewTopic  bool
}

// Window is the resolver input (chapter windows only).
type Window struct {
	Title    string
	Topics   []string
	Entities []string
}

// Catalog is the identity lookup the resolver mutates.
type Catalog interface {
	Lookup(norm string) (slug, title string, ok bool)
	EnsureTopic(slug, title, origin string) (titleOut string, created bool)
	EnsureAlias(norm, slug, raw, source string)
}

// Resolve maps a window's labels onto catalog identities.
func Resolve(w Window, cat Catalog) []Bind {
	if cat == nil {
		return nil
	}
	type cand struct {
		raw, kind string
	}
	var cands []cand
	for _, t := range w.Topics {
		cands = append(cands, cand{t, "topic_label"})
	}
	for _, e := range w.Entities {
		cands = append(cands, cand{e, "entity"})
	}
	if title := strings.TrimSpace(w.Title); title != "" {
		cands = append(cands, cand{title, "title"})
	}

	out := make([]Bind, 0, 4)
	seen := map[string]bool{}
	add := func(b Bind) {
		if b.Slug == "" || seen[b.Slug] {
			return
		}
		seen[b.Slug] = true
		out = append(out, b)
	}

	for _, c := range cands {
		raw := strings.TrimSpace(c.raw)
		if raw == "" {
			continue
		}
		if c.kind == "title" {
			for _, b := range resolveTitle(raw, cat) {
				add(b)
			}
			continue
		}
		if b, ok := resolveLabel(raw, c.kind, cat); ok {
			add(b)
		}
	}
	return out
}

func resolveLabel(raw, kind string, cat Catalog) (Bind, bool) {
	norm := Normalize(raw)
	if norm == "" {
		return Bind{}, false
	}
	if slug, title, ok := cat.Lookup(norm); ok {
		match := kind
		if match == "topic_label" {
			match = "alias"
		}
		return Bind{Slug: slug, Title: title, Raw: raw, MatchKind: match}, true
	}
	if kind == "entity" {
		return Bind{}, false
	}
	if IsProcedural(raw) {
		return Bind{}, false
	}
	title := displayTitle(raw)
	slug := wiki.SlugFromName(title)
	if slug == "" {
		return Bind{}, false
	}
	titleOut, created := cat.EnsureTopic(slug, title, "resolver")
	cat.EnsureAlias(norm, slug, raw, "window_label")
	cat.EnsureAlias(slug, slug, title, "window_label")
	return Bind{Slug: slug, Title: titleOut, Raw: raw, MatchKind: kind, NewTopic: created}, true
}

func resolveTitle(title string, cat Catalog) []Bind {
	var out []Bind
	seen := map[string]bool{}
	try := func(raw string) {
		norm := Normalize(raw)
		if norm == "" {
			return
		}
		slug, t, ok := cat.Lookup(norm)
		if !ok || seen[slug] {
			return
		}
		seen[slug] = true
		out = append(out, Bind{Slug: slug, Title: t, Raw: title, MatchKind: "title"})
	}
	try(title)
	for _, part := range titleParts(title) {
		try(part)
	}
	return out
}

// Normalize folds a label to a flat alias key.
func Normalize(s string) string {
	return wiki.SlugFromName(s)
}

func displayTitle(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	words := strings.Fields(raw)
	for i, w := range words {
		if len(w) == 0 {
			continue
		}
		if allUpper(w) && len(w) <= 5 {
			continue
		}
		r := []rune(strings.ToLower(w))
		r[0] = unicode.ToUpper(r[0])
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}

func allUpper(s string) bool {
	letters := 0
	for _, r := range s {
		if unicode.IsLetter(r) {
			letters++
			if !unicode.IsUpper(r) {
				return false
			}
		}
	}
	return letters > 0
}

func titleParts(title string) []string {
	var parts []string
	buf := strings.Builder{}
	flush := func() {
		s := strings.TrimSpace(buf.String())
		buf.Reset()
		if s != "" {
			parts = append(parts, s)
		}
	}
	for _, r := range title {
		if r == '/' || r == ':' || r == ',' || r == ';' || r == '&' || r == '|' {
			flush()
			continue
		}
		if unicode.IsSpace(r) || r == '-' {
			flush()
			continue
		}
		buf.WriteRune(r)
	}
	flush()
	return parts
}
