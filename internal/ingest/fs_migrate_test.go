package ingest

import "testing"

func TestMigrateLeavesAbsentPathAlone(t *testing.T) {
	const key = "org/tenant/uid/uid.video.mp4"
	got, thumb, err := migrateVideoAssetsToCanonicalDir(t.Context(), "vid", key, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != key || thumb != nil {
		t.Fatalf("got %q thumb %#v", got, thumb)
	}
}
