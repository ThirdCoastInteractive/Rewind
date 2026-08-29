package automarkers

import "testing"

func TestDescriptionChapters(t *testing.T) {
	desc := "Intro stuff\n0:00 Cold open\n1:23 The rant\n12:00 Outro"
	got := descriptionChapters(desc, 800)
	if len(got) != 3 {
		t.Fatalf("got %#v", got)
	}
	if got[1].Title != "The rant" || got[1].Start != 83 {
		t.Errorf("%+v", got[1])
	}
}

func TestDescriptionChaptersRequiresList(t *testing.T) {
	if got := descriptionChapters("see this at 1:23 wow", 100); got != nil {
		t.Fatalf("single timestamp should not be a chapter list: %#v", got)
	}
}
