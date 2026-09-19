package typefaces

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSlug(t *testing.T) {
	t.Parallel()
	if got := slug("Playfair Display"); got != "playfair-display" {
		t.Fatalf("got %q", got)
	}
	if got := slug("UnifrakturCook"); got != "unifrakturcook" {
		t.Fatalf("got %q", got)
	}
}

func TestLoadFamily(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	regular := filepath.Join(dir, "latin-400-normal.ttf")
	if err := os.WriteFile(regular, make([]byte, 2048), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := familyMeta{
		ID:     "playfair-display",
		Family: "Playfair Display",
		Files:  []familyFile{{Weight: 400, Style: "normal", Path: "latin-400-normal.ttf"}},
	}
	if err := writeFamilyMeta(dir, meta); err != nil {
		t.Fatal(err)
	}
	f, err := loadFamily(dir)
	if err != nil {
		t.Fatal(err)
	}
	if f.Family != "Playfair Display" || f.Regular == "" {
		t.Fatalf("%+v", f)
	}
	if f.Bold != f.Regular {
		t.Fatalf("bold should fall back to regular: %+v", f)
	}
}

func TestFileUsesFONTS_DIR(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FONTS_DIR", root)
	fam := filepath.Join(root, "google", "inter")
	if err := os.MkdirAll(fam, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fam, "latin-700-normal.ttf"), make([]byte, 2048), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeFamilyMeta(fam, familyMeta{
		ID: "inter", Family: "Inter",
		Files: []familyFile{{Weight: 700, Style: "normal", Path: "latin-700-normal.ttf"}},
	}); err != nil {
		t.Fatal(err)
	}
	p := File("Inter", true)
	if p == "" {
		t.Fatal("expected path")
	}
	f, ok := Lookup("Inter")
	if !ok || f.ID != "inter" {
		t.Fatalf("lookup %+v %v", f, ok)
	}
}
