package ffmpeg

import (
	"os"
	"strings"
	"testing"

	"thirdcoast.systems/rewind/pkg/captions"
)

func TestSliceCuesForBurn(t *testing.T) {
	t.Parallel()
	cues := []captions.Cue{
		{Start: 10, End: 12, Text: "hello there"},
		{Start: 11, End: 14, Text: "♪ music ♪"},
		{Start: 13, End: 15, Text: "[laughter]"},
		{Start: 20, End: 22, Text: "after the clip"},
		{Start: 9.5, End: 11.5, Text: "overlap start"},
	}
	got := sliceCuesForBurn(cues, 10, 16)
	if len(got) != 2 {
		t.Fatalf("got %#v", got)
	}
	if got[0].Text != "hello there" || got[0].Start != 0 || got[0].End != 2 {
		t.Fatalf("first %#v", got[0])
	}
	if got[1].Text != "overlap start" || got[1].Start != 0 {
		t.Fatalf("overlap %#v", got[1])
	}
}

func TestWriteBurnASS(t *testing.T) {
	t.Parallel()
	cues := []captions.Cue{{Start: 0, End: 2.5, Text: "CALLEN'S TESLA"}}
	body := burnASSDocument(cues, "Tomorrow")
	if !strings.Contains(body, "Fontname, Fontsize") {
		t.Fatal("missing style header")
	}
	if !strings.Contains(body, "Tomorrow") {
		t.Fatal("expected Tomorrow")
	}
	if !strings.Contains(body, "CALLEN'S TESLA") {
		t.Fatal("missing dialogue")
	}
	if !strings.Contains(body, "0:00:00.00") || !strings.Contains(body, "0:00:02.50") {
		t.Fatalf("times: %s", body)
	}
	p, err := writeBurnASS(cues, "Tomorrow")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "Dialogue:") {
		t.Fatal(string(b))
	}
}

func TestCaptionFontBundledGothic(t *testing.T) {
	name, dir, err := captionFont("UnifrakturCook")
	if err != nil {
		t.Fatal(err)
	}
	if name != "UnifrakturCook" {
		t.Fatalf("name %s", name)
	}
	if dir == "" {
		t.Fatal("expected bundled font dir")
	}
}

func TestAssDialogueEscapes(t *testing.T) {
	t.Parallel()
	got := assDialogueText("foo {bar}\nbaz")
	if got != `foo \{bar\}\Nbaz` {
		t.Fatalf("got %q", got)
	}
}
