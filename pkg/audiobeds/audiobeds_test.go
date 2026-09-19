package audiobeds

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileUsesAUDIO_DIR(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUDIO_DIR", root)
	p := filepath.Join(root, "dies-irae-open.ogg")
	if err := os.WriteFile(p, make([]byte, 2048), 0o644); err != nil {
		t.Fatal(err)
	}
	got := File("dies-irae-open")
	if got != p {
		t.Fatalf("got %q want %q", got, p)
	}
	if File("missing") != "" {
		t.Fatal("expected empty")
	}
}
