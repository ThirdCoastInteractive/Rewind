package video_api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/pkg/plugin"
	"thirdcoast.systems/rewind/pkg/plugin/builtin"
)

func TestRemoveLocalDirSkipsMissingR2Masters(t *testing.T) {
	if removeLocalDir(true, false) {
		t.Fatal("R2 masters have no local directory; delete_disk must not refuse the row delete")
	}
	if !removeLocalDir(true, true) {
		t.Fatal("a safe local directory should be removed when delete_disk is set")
	}
	if removeLocalDir(false, true) {
		t.Fatal("without delete_disk the local directory stays")
	}
}

func TestSafeVideoDirForDeletionUsesBlobRoot(t *testing.T) {
	plugin.Reset()
	t.Cleanup(plugin.Reset)

	root := t.TempDir()
	var id pgtype.UUID
	if err := id.Scan("11111111-1111-1111-1111-111111111111"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, id.String())
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	plugin.Use(plugin.Set{Blob: builtin.NewDisk(root)})

	got, ok := safeVideoDirForDeletion(id)
	if !ok || got != dir {
		t.Fatalf("got %q ok=%v want %q", got, ok, dir)
	}

	outside := t.TempDir()
	plugin.Use(plugin.Set{Blob: builtin.NewDisk(outside)})
	if _, ok := safeVideoDirForDeletion(id); ok {
		t.Fatal("must not delete a directory outside the blob root")
	}
}
