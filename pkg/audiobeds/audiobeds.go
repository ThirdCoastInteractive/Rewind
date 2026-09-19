// Package audiobeds lists uploaded and seeded music beds for stitch intro/outro cards.
package audiobeds

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Bed is one audio file on disk.
type Bed struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path,omitempty"`
	Ext  string `json:"ext,omitempty"`
}

var audioExt = map[string]bool{
	".ogg": true, ".oga": true, ".mp3": true, ".wav": true,
	".flac": true, ".m4a": true, ".aac": true, ".opus": true,
}

// Dir is AUDIO_DIR, /audio in the container, or ./bin/audio on the host.
func Dir() string {
	if d := strings.TrimSpace(os.Getenv("AUDIO_DIR")); d != "" {
		return d
	}
	if st, err := os.Stat("/audio"); err == nil && st.IsDir() {
		return "/audio"
	}
	return filepath.Join("bin", "audio")
}

// List returns seeded + uploaded beds, seeded first.
func List() []Bed {
	_ = os.MkdirAll(Dir(), 0o755)
	var out []Bed
	seen := map[string]bool{}
	addDir := func(root string) {
		ents, err := os.ReadDir(root)
		if err != nil {
			return
		}
		for _, e := range ents {
			if e.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if !audioExt[ext] {
				continue
			}
			id := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
			id = slug(id)
			if id == "" || seen[id] {
				continue
			}
			p := filepath.Join(root, e.Name())
			if st, err := os.Stat(p); err != nil || st.Size() < 256 {
				continue
			}
			seen[id] = true
			out = append(out, Bed{ID: id, Name: displayName(id), Path: p, Ext: ext})
		}
	}
	addDir(Dir())
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// File returns an absolute path for a bed id or filename.
func File(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if filepath.IsAbs(id) {
		if st, err := os.Stat(id); err == nil && st.Size() > 0 {
			return id
		}
		return ""
	}
	want := slug(strings.TrimSuffix(id, filepath.Ext(id)))
	for _, b := range List() {
		if b.ID == want || strings.EqualFold(b.Name, id) {
			return b.Path
		}
	}
	return ""
}

func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prev := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prev = false
		default:
			if !prev && b.Len() > 0 {
				b.WriteByte('-')
				prev = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func displayName(id string) string {
	parts := strings.Split(id, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}
