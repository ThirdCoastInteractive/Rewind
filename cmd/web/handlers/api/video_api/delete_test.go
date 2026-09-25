package video_api

import "testing"

func TestLibraryRowDeletable(t *testing.T) {
	if libraryRowDeletable(false, false, false) {
		t.Fatal("OSS non-owner must not delete")
	}
	if !libraryRowDeletable(true, false, false) {
		t.Fatal("admin")
	}
	if !libraryRowDeletable(false, true, false) {
		t.Fatal("owner")
	}
	if !libraryRowDeletable(false, false, true) {
		t.Fatal("live workspace write")
	}
}
