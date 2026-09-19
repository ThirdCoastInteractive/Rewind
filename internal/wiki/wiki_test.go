package wiki

import (
	"strings"
	"testing"
)

func TestSlug(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"The Terminal", "the-terminal"},
		{"Ben Avery", "ben-avery"},
		{"ben-avery/2026-09-03", "ben-avery/2026-09-03"},
		{"Ben Avery/2026-09-03!!", "ben-avery/2026-09-03"},
		{"  /Foo//Bar/  ", "foo/bar"},
		{"", ""},
		{"!!!", ""},
	}
	for _, c := range cases {
		if got := Slug(c.in); got != c.want {
			t.Fatalf("Slug(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestValidTree(t *testing.T) {
	for _, tree := range Trees {
		if !ValidTree(tree) {
			t.Fatalf("expected %q valid", tree)
		}
	}
	if ValidTree("lore") || ValidTree("") || ValidTree("Creator") {
		t.Fatal("unexpected valid tree")
	}
}

func TestParseLinks(t *testing.T) {
	body := `See [[clipping/workflow]] and [[ben-avery/2026-09-03|tonight]] plus [[Notes]].`
	links := ParseLinks(body, TreeCreator)
	if len(links) != 3 {
		t.Fatalf("got %d links: %#v", len(links), links)
	}
	if links[0].Tree != TreeClipping || links[0].Slug != "workflow" {
		t.Fatalf("cross-tree: %#v", links[0])
	}
	if links[1].Tree != TreeCreator || links[1].Slug != "ben-avery/2026-09-03" || links[1].Label != "tonight" {
		t.Fatalf("nested default tree: %#v", links[1])
	}
	if links[2].Tree != TreeCreator || links[2].Slug != "notes" {
		t.Fatalf("bare slug: %#v", links[2])
	}

	dupes := ParseLinks(`[[Notes]] then [[notes]] again`, TreeTopic)
	if len(dupes) != 1 {
		t.Fatalf("dedupe: %#v", dupes)
	}
}

func TestParentSlug(t *testing.T) {
	cases := []struct{ in, want string }{
		{"ben-avery", ""},
		{"ben-avery/2026-09-03", "ben-avery"},
		{"a/b/c", "a/b"},
		{"", ""},
	}
	for _, c := range cases {
		if got := ParentSlug(c.in); got != c.want {
			t.Fatalf("ParentSlug(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestGroupPages(t *testing.T) {
	pages := []Page{
		{Slug: "ben-avery", Title: "Ben"},
		{Slug: "ben-avery/2026-09-03", Title: "Sep 3"},
		{Slug: "truthseekershow", Title: "Truth"},
		{Slug: "orphan/child", Title: "Orphan child"},
	}
	groups := GroupPages(pages)
	if len(groups) != 3 {
		t.Fatalf("roots=%d want 3: %#v", len(groups), groups)
	}
	var ben PageGroup
	for _, g := range groups {
		if g.Page.Slug == "ben-avery" {
			ben = g
		}
	}
	if len(ben.Children) != 1 || ben.Children[0].Page.Slug != "ben-avery/2026-09-03" {
		t.Fatalf("ben children: %#v", ben.Children)
	}
}

func TestUnifiedDiff(t *testing.T) {
	if UnifiedDiff("", "brand new") != "" {
		t.Fatal("create should yield empty diff")
	}
	diff := UnifiedDiff("one\ntwo\n", "one\nthree\n")
	if !strings.Contains(diff, "-two") || !strings.Contains(diff, "+three") {
		t.Fatalf("unexpected diff:\n%s", diff)
	}
}
