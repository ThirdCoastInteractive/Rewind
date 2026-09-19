// Package typefaces lists bundled and downloaded fonts for titles and captions.
// Google families are pulled as TTF via Fontsource (the Google Fonts corpus).
package typefaces

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Face is one installed family with at least a regular or bold TTF.
type Face struct {
	ID       string `json:"id"`
	Family   string `json:"family"`
	Source   string `json:"source"` // bundled | google
	Regular  string `json:"regular,omitempty"`
	Bold     string `json:"bold,omitempty"`
	Dir      string `json:"dir,omitempty"`
	Category string `json:"category,omitempty"`
}

type familyMeta struct {
	ID       string       `json:"id"`
	Family   string       `json:"family"`
	Category string       `json:"category,omitempty"`
	Files    []familyFile `json:"files"`
}

type familyFile struct {
	Weight int    `json:"weight"`
	Style  string `json:"style"`
	Path   string `json:"path"`
}

// Dir is the persistent downloaded-font root (FONTS_DIR, default /fonts in
// the container or ./bin/fonts for a host binary).
func Dir() string {
	if d := strings.TrimSpace(os.Getenv("FONTS_DIR")); d != "" {
		return d
	}
	if st, err := os.Stat("/fonts"); err == nil && st.IsDir() {
		return "/fonts"
	}
	return filepath.Join("bin", "fonts")
}

func googleDir() string {
	return filepath.Join(Dir(), "google")
}

// Bundled are the families shipped in the image.
func Bundled() []Face {
	return []Face{
		{ID: "tomorrow", Family: "Tomorrow", Source: "bundled", Category: "sans-serif"},
		{ID: "unifraktur-cook", Family: "UnifrakturCook", Source: "bundled", Category: "display"},
	}
}

// Installed is bundled plus every downloaded Google family.
func Installed() []Face {
	out := Bundled()
	root := googleDir()
	ents, err := os.ReadDir(root)
	if err != nil {
		return out
	}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		f, err := loadFamily(filepath.Join(root, e.Name()))
		if err != nil {
			continue
		}
		out = append(out, f)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source == "bundled"
		}
		return strings.ToLower(out[i].Family) < strings.ToLower(out[j].Family)
	})
	return out
}

// File returns a TTF path for family. bold prefers 700 then the heaviest file.
func File(family string, bold bool) string {
	family = strings.TrimSpace(family)
	if family == "" {
		return ""
	}
	for _, f := range Installed() {
		if !sameFamily(f, family) {
			continue
		}
		if bold {
			if f.Bold != "" {
				return f.Bold
			}
			return f.Regular
		}
		if f.Regular != "" {
			return f.Regular
		}
		return f.Bold
	}
	return ""
}

// InstalledIndex is family/id/slug → true for picker Use vs Install.
func InstalledIndex() map[string]bool {
	m := map[string]bool{}
	for _, f := range Installed() {
		m[strings.ToLower(f.Family)] = true
		m[f.ID] = true
		m[slug(f.Family)] = true
	}
	return m
}

// Lookup returns the installed face for a family name or id.
func Lookup(family string) (Face, bool) {
	family = strings.TrimSpace(family)
	for _, f := range Installed() {
		if sameFamily(f, family) {
			return f, true
		}
	}
	return Face{}, false
}

func sameFamily(f Face, name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return strings.ToLower(f.Family) == n || f.ID == n || slug(f.Family) == n
}

func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func loadFamily(dir string) (Face, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "family.json"))
	if err != nil {
		return Face{}, err
	}
	var meta familyMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return Face{}, err
	}
	f := Face{
		ID:       meta.ID,
		Family:   meta.Family,
		Source:   "google",
		Dir:      dir,
		Category: meta.Category,
	}
	var regular, bold string
	bestRegDist, bestBold := 999, -1
	for _, file := range meta.Files {
		if !strings.EqualFold(file.Style, "normal") {
			continue
		}
		p := file.Path
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		if st, err := os.Stat(p); err != nil || st.Size() == 0 {
			continue
		}
		dist := file.Weight - 400
		if dist < 0 {
			dist = -dist
		}
		if dist < bestRegDist {
			bestRegDist = dist
			regular = p
		}
		if file.Weight >= 600 && file.Weight > bestBold {
			bestBold = file.Weight
			bold = p
		}
	}
	f.Regular = regular
	f.Bold = bold
	if f.Bold == "" {
		f.Bold = f.Regular
	}
	if f.Regular == "" && f.Bold == "" {
		return Face{}, os.ErrNotExist
	}
	return f, nil
}

func writeFamilyMeta(dir string, meta familyMeta) error {
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "family.json"), b, 0o644)
}
