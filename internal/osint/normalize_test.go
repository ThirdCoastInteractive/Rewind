package osint

import "testing"

func TestNormalizeTextStripsURLsAndMentions(t *testing.T) {
	in := "Hello @Alice check https://example.com/foo  and   www.test.org thanks"
	got := NormalizeText(in)
	want := "hello check and thanks"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestNormalizeTextCollapseSpace(t *testing.T) {
	got := NormalizeText("  One   Two\tThree\n")
	if got != "one two three" {
		t.Fatalf("got %q", got)
	}
}
