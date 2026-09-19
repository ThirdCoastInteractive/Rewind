package wiki

import "testing"

func TestRewriteWikilinks(t *testing.T) {
	got := RewriteWikilinks("See [[ben-avery]] and [[creator/stella-lefty|Stella]].", TreeClipping)
	wantA := "[ben-avery](/wiki/clipping/ben-avery)"
	wantB := "[Stella](/wiki/creator/stella-lefty)"
	if !contains(got, wantA) || !contains(got, wantB) {
		t.Fatalf("got %q", got)
	}
}

func TestResolveNestedSlug(t *testing.T) {
	tree, slug := ResolveLink(TreeClipping, "clipping/ben-avery/2026-09-03")
	if tree != TreeClipping || slug != "ben-avery/2026-09-03" {
		t.Fatalf("tree=%q slug=%q", tree, slug)
	}
}

func TestRenderHTML(t *testing.T) {
	html := RenderHTML("# Hello\n\nA **bold** [[topic/pizza]] link.", TreeTopic)
	for _, need := range []string{"<h1", "Hello", "<strong>bold</strong>", `href="/wiki/topic/pizza"`} {
		if !contains(html, need) {
			t.Fatalf("missing %q in %s", need, html)
		}
	}
}

func TestHeadings(t *testing.T) {
	html := RenderHTML("# Title\n\n## Slots\n\n### Setup\n\n## Guest", TreeClipping)
	hs := ArticleHeadings(html)
	if len(hs) != 3 {
		t.Fatalf("got %d headings: %#v\nhtml=%s", len(hs), hs, html)
	}
	if hs[0].Text != "Slots" || hs[0].Level != 2 || hs[0].ID == "" {
		t.Fatalf("slots: %#v", hs[0])
	}
	if hs[1].Text != "Setup" || hs[1].Level != 3 {
		t.Fatalf("setup: %#v", hs[1])
	}
	if hs[2].Text != "Guest" {
		t.Fatalf("guest: %#v", hs[2])
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
