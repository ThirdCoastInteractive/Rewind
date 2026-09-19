package stitch

import (
	"strings"
	"testing"
)

func TestNormalizeYouTubeTrimAndEmpty(t *testing.T) {
	desc, tags, err := NormalizeYouTube("  hello  ", []string{"  a  ", "", "  "})
	if err != nil {
		t.Fatal(err)
	}
	if desc != "hello" {
		t.Fatalf("desc=%q", desc)
	}
	if len(tags) != 1 || tags[0] != "a" {
		t.Fatalf("tags=%v", tags)
	}
}

func TestNormalizeYouTubeDuplicateCollapse(t *testing.T) {
	_, tags, err := NormalizeYouTube("", []string{"Foo", "foo", "FOO", "Bar"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 2 || tags[0] != "Foo" || tags[1] != "Bar" {
		t.Fatalf("tags=%v", tags)
	}
}

func TestNormalizeYouTubeNilTags(t *testing.T) {
	_, tags, err := NormalizeYouTube("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if tags == nil || len(tags) != 0 {
		t.Fatalf("tags=%#v", tags)
	}
}

func TestNormalizeYouTubeMaxTags(t *testing.T) {
	in := make([]string, MaxYouTubeTags+1)
	for i := range in {
		in[i] = string(rune('a' + i%26)) + strings.Repeat("x", i/26+1)
	}
	if _, _, err := NormalizeYouTube("", in); err == nil {
		t.Fatal("expected max tags error")
	}
	in = in[:MaxYouTubeTags]
	if _, tags, err := NormalizeYouTube("", in); err != nil || len(tags) != MaxYouTubeTags {
		t.Fatalf("exact max: %v %d", err, len(tags))
	}
}

func TestNormalizeYouTubeMaxTagLen(t *testing.T) {
	ok := strings.Repeat("ä", MaxYouTubeTagLen)
	if _, tags, err := NormalizeYouTube("", []string{ok}); err != nil || len(tags) != 1 {
		t.Fatalf("exact max len: %v %v", err, tags)
	}
	if _, _, err := NormalizeYouTube("", []string{ok + "x"}); err == nil {
		t.Fatal("expected tag length error")
	}
}

func TestNormalizeYouTubeMaxDescription(t *testing.T) {
	ok := strings.Repeat("界", MaxYouTubeDescription)
	if desc, _, err := NormalizeYouTube(ok, nil); err != nil || desc != ok {
		t.Fatalf("exact max desc: %v", err)
	}
	if _, _, err := NormalizeYouTube(ok+"!", nil); err == nil {
		t.Fatal("expected description length error")
	}
}
