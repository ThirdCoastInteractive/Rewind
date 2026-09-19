package ingest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindLiveChatFile(t *testing.T) {
	dir := t.TempDir()
	if p := findLiveChatFile(dir); p != "" {
		t.Fatalf("empty dir: %s", p)
	}
	path := filepath.Join(dir, "youtube_abc.live_chat.json")
	if err := os.WriteFile(path, []byte(`{"id":"1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := findLiveChatFile(dir); got != path {
		t.Fatalf("got %q want %q", got, path)
	}
}
