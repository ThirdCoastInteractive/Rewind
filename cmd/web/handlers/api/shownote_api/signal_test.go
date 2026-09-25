package shownote_api

import (
	"testing"

	"thirdcoast.systems/rewind/internal/db"
)

func TestPublicViewerCapabilityRequiresLiveNote(t *testing.T) {
	if publicViewerAllowed(&db.ShowNote{IsLive: true}) == false {
		t.Fatal("live note must be a public viewer capability")
	}
	if publicViewerAllowed(&db.ShowNote{}) {
		t.Fatal("offline note must be denied")
	}
	if publicViewerAllowed(nil) {
		t.Fatal("missing note must be denied")
	}
}
