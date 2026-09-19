package encode

import (
	"strings"
	"testing"

	"thirdcoast.systems/rewind/internal/stitch"
)

func TestCompileCanonicalCaptionsFormatsAndEscapes(t *testing.T) {
	d := stitch.Document{Width: 1920, Height: 1080, Captions: []stitch.Caption{{ID: "c", Text: "héllo {world}\nnow", StartUS: 1_250_000, EndUS: 3_500_000, Style: stitch.CaptionStyle{Font: "Noto", FontSize: 42, Color: "#ff0000", OutlineColor: "#001122", OutlineWidth: 2}}}}
	srt, err := CompileCanonicalCaptions(d, "srt", false)
	if err != nil || !strings.Contains(srt, "00:00:01,250 --> 00:00:03,500") || !strings.Contains(srt, "héllo {world}") {
		t.Fatalf("srt=%q err=%v", srt, err)
	}
	vtt, err := CompileCanonicalCaptions(d, "vtt", false)
	if err != nil || !strings.Contains(vtt, "00:00:01.250 --> 00:00:03.500") {
		t.Fatalf("vtt=%q err=%v", vtt, err)
	}
	ass, err := CompileCanonicalCaptions(d, "ass", false)
	if err != nil || !strings.Contains(ass, `\fnNoto`) || !strings.Contains(ass, `\{world\}`) || !strings.Contains(ass, "0:00:01.25") {
		t.Fatalf("ass=%q err=%v", ass, err)
	}
}

func TestCompileCanonicalCaptionsFallsBackToSettingsFont(t *testing.T) {
	d := stitch.Document{
		Width: 1920, Height: 1080,
		Settings: stitch.Settings{CaptionFont: "Bebas Neue"},
		Captions: []stitch.Caption{{ID: "c", Text: "hi", StartUS: 0, EndUS: 1_000_000}},
	}
	ass, err := CompileCanonicalCaptions(d, "ass", false)
	if err != nil || !strings.Contains(ass, "Style: Default,Bebas Neue,") || !strings.Contains(ass, `\fnBebas Neue`) {
		t.Fatalf("ass=%q err=%v", ass, err)
	}
}

func TestCompileCanonicalCaptionsWordHighlightRequiresValidWords(t *testing.T) {
	d := stitch.Document{Captions: []stitch.Caption{{ID: "c", Text: "hello world", StartUS: 0, EndUS: 2_000_000, Style: stitch.CaptionStyle{WordHighlight: true}, Words: []stitch.Word{{Text: "hello", StartUS: 0, EndUS: 1_000_000}}}}}
	if _, err := CompileCanonicalCaptions(d, "ass", true); err == nil {
		t.Fatal("expected pending alignment error")
	}
	d.Captions[0].Alignment = "valid"
	d.Captions[0].Words = append(d.Captions[0].Words, stitch.Word{Text: "world", StartUS: 1_000_000, EndUS: 2_000_000})
	ass, err := CompileCanonicalCaptions(d, "ass", true)
	if err != nil || !strings.Contains(strings.ToUpper(ass), "0000FFFF") || strings.Count(ass, "Dialogue:") < 2 {
		t.Fatalf("ass=%q err=%v", ass, err)
	}
}
