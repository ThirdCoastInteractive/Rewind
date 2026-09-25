package fileserver

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"thirdcoast.systems/rewind/pkg/plugin"
	"thirdcoast.systems/rewind/pkg/plugin/builtin"
)

func TestGetVideoDirForIDUsesBlobRoot(t *testing.T) {
	plugin.Reset()
	t.Cleanup(plugin.Reset)

	pluginRoot := t.TempDir()
	legacy := t.TempDir()
	videoID := "11111111-1111-1111-1111-111111111111"
	if err := os.Mkdir(filepath.Join(legacy, videoID), 0o755); err != nil {
		t.Fatal(err)
	}

	plugin.Use(plugin.Set{Blob: builtin.NewDisk(pluginRoot)})
	t.Setenv("DOWNLOADS_DIR", legacy)

	got, err := GetVideoDirForID(context.Background(), videoID)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(pluginRoot, videoID)
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}
