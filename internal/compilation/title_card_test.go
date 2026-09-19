package compilation

import "testing"

func TestGothicTitleCardMatchesChapterStyle(t *testing.T) {
	card := gothicTitleCard("Chapter 1: Staying Positive", "Ben Avery")
	if card["type"] != "title" || card["text"] != "Chapter 1: Staying Positive" {
		t.Fatalf("card: %#v", card)
	}
	if card["font"] != "UnifrakturCook" || card["text_color"] != "#f5f0e6" || card["duration"] != 4.0 {
		t.Fatalf("gothic defaults: %#v", card)
	}
	if card["subtitle"] != "Ben Avery" {
		t.Fatalf("subtitle: %#v", card)
	}
}

func TestWithGothicTitleCardPrependsOnce(t *testing.T) {
	clips := []map[string]any{{"type": "clip", "title": "beat"}}
	once := withGothicTitleCard("Chapter 1: Staying Positive", "Ben Avery", clips)
	if len(once) != 2 || once[0]["type"] != "title" || once[1]["type"] != "clip" {
		t.Fatalf("prepend: %#v", once)
	}
	twice := withGothicTitleCard("Chapter 1: Staying Positive", "Ben Avery", once)
	if len(twice) != 2 {
		t.Fatalf("should not duplicate title cards: %#v", twice)
	}
	if withGothicTitleCard("", "", clips)[0]["type"] != "clip" {
		t.Fatal("empty title should leave clips unchanged")
	}
}
