package encode

import (
	"strings"
	"testing"

	"thirdcoast.systems/rewind/internal/stitch"
)

func TestCompileCanonicalOverlaysAllVectorKinds(t *testing.T) {
	d := stitch.Document{Width: 1000, Height: 500, Overlays: []stitch.Overlay{
		{ID: "text", Kind: "text", Text: "héllo {world}", StartUS: 0, EndUS: 1_000_000, Visible: true, X: 100, Y: 100, Width: 400, Height: 50, Opacity: .8, Rotation: 12, Z: 3, Font: "Noto", FontSize: 30},
		{ID: "callout", Kind: "callout", Text: "note", Width: 200, Height: 100, StartUS: 0, EndUS: 1_000_000, Visible: true, Z: 4},
		{ID: "arrow", Kind: "arrow", StartUS: 0, EndUS: 1_000_000, Visible: true, Width: 200, Height: 100, Z: 5},
		{ID: "rect", Kind: "rectangle", StartUS: 0, EndUS: 1_000_000, Visible: true, Width: .2, Height: .1, Z: 1},
		{ID: "ellipse", Kind: "ellipse", StartUS: 0, EndUS: 1_000_000, Visible: true, Width: .2, Height: .1, Z: 2},
		{ID: "free", Kind: "freehand", StartUS: 0, EndUS: 1_000_000, Visible: true, Z: 6, Points: []stitch.Point{{X: .1, Y: .1}, {X: .2, Y: .2}}},
	}}
	got, err := CompileCanonicalOverlays(d)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(got, "Dialogue:") != 8 || !strings.Contains(got, "héllo \\{world\\}") || !strings.Contains(got, "\\p1") || !strings.Contains(got, "\\pos(100.00,100.00)") || !strings.Contains(got, "\\pos(24.00,24.00)") || !strings.Contains(got, " b ") {
		t.Fatalf("overlay ASS missing fields: %s", got)
	}
}

func TestCompileCanonicalOverlaysRejectsImagesAndUnknowns(t *testing.T) {
	for _, o := range []stitch.Overlay{{ID: "i", Kind: "image", AssetID: "asset", Visible: true, EndUS: 1}, {ID: "x", Kind: "mystery", Visible: true, EndUS: 1}} {
		if _, err := CompileCanonicalOverlays(stitch.Document{Width: 1, Height: 1, Overlays: []stitch.Overlay{o}}); err == nil {
			t.Fatalf("expected error for %s", o.Kind)
		}
	}
}
