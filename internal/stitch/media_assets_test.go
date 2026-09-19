package stitch

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestValidateAssetCorruptPNGHeaderRejected(t *testing.T) {
	b := []byte("\x89PNG\r\n\x1a\n")
	if _, _, _, e := ValidateAsset(bytes.NewReader(b)); e == nil {
		t.Fatal("corrupt png accepted")
	}
}
func TestValidateAssetValidPNGAccepted(t *testing.T) {
	im := image.NewRGBA(image.Rect(0, 0, 2, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 2; x++ {
			im.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	var b bytes.Buffer
	if e := png.Encode(&b, im); e != nil {
		t.Fatal(e)
	}
	got, format, hash, e := ValidateAsset(bytes.NewReader(b.Bytes()))
	if e != nil || len(got) == 0 || format != "png" || len(hash) != 64 {
		t.Fatalf("valid png: %v %q %q", e, format, hash)
	}
}
