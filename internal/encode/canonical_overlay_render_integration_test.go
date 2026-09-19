//go:build integration

package encode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"thirdcoast.systems/rewind/internal/stitch"
)

func TestApplyCanonicalOverlaysFFmpegFixture(t *testing.T) {
	ffmpeg := toolPath(t, "ffmpeg")
	dir := t.TempDir()
	input := filepath.Join(dir, "input.mp4")
	output := filepath.Join(dir, "output.mp4")
	runTool(t, ffmpeg, "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", "color=c=black:s=100x100:r=30:d=2", "-c:v", "libx264", "-pix_fmt", "yuv420p", input)
	inputData, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(output, inputData, 0o600); err != nil {
		t.Fatal(err)
	}
	assetPath := filepath.Join(dir, "red.png")
	img := image.NewNRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 255, A: 255})
		}
	}
	f, err := os.Create(assetPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	data, _ := os.ReadFile(assetPath)
	sum := sha256.Sum256(data)
	asset, _ := json.Marshal(map[string]any{"id": "asset-1", "path": assetPath, "hash": hex.EncodeToString(sum[:]), "size": len(data)})
	snap := stitch.RenderSnapshot{Document: stitch.Document{Version: 1, Width: 100, Height: 100, Overlays: []stitch.Overlay{
		{ID: "rect", Kind: "rectangle", X: 10, Y: 10, Width: 20, Height: 20, Color: "#ff0000", Background: "#ff0000", Opacity: 1, StartUS: 0, EndUS: 2_000_000, Visible: true, Z: 1},
		{ID: "text", Kind: "text", Text: "hé", X: 45, Y: 40, Width: 30, Height: 20, Color: "#ffffff", Font: "Arial", FontSize: 16, Opacity: 1, StartUS: 0, EndUS: 2_000_000, Visible: true, Z: 2},
		{ID: "image", Kind: "image", AssetID: "asset-1", X: 60, Y: 10, Width: 20, Height: 20, Opacity: .5, StartUS: 0, EndUS: 2_000_000, Visible: true, Z: 3},
	}}, Assets: []json.RawMessage{asset}}
	if err := applyCanonicalOverlays(context.Background(), output, snap); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(output); err != nil || st.Size() == 0 {
		t.Fatalf("output missing: %v", err)
	}
	probe := toolPath(t, "ffprobe")
	out := runTool(t, probe, "-v", "error", "-show_entries", "format=duration", "-of", "default=nw=1:nk=1", output)
	if !strings.HasPrefix(strings.TrimSpace(out), "2.") {
		t.Fatalf("duration=%q", out)
	}
	frame := filepath.Join(dir, "frame.png")
	runTool(t, ffmpeg, "-hide_banner", "-loglevel", "error", "-y", "-ss", "1", "-i", output, "-frames:v", "1", frame)
	f, err = os.Open(frame)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(f)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	var rectangleRed bool
	for y := 5; y < 40 && !rectangleRed; y++ {
		for x := 5; x < 40; x++ {
			r, g, b, _ := decoded.At(x, y).RGBA()
			if r > 50000 && g < 12000 && b < 12000 {
				rectangleRed = true
				break
			}
		}
	}
	if !rectangleRed {
		t.Fatal("rectangle overlay produced no red pixels")
	}
	blend := decoded.At(65, 15)
	if r, g, b, _ := blend.RGBA(); r < 25000 || r > 60000 || g > 12000 || b > 12000 {
		t.Fatalf("alpha image pixel=%v", blend)
	}
	var textPixel bool
	for y := 40; y < 60; y++ {
		for x := 45; x < 75; x++ {
			r, g, b, _ := decoded.At(x, y).RGBA()
			if r > 12000 || g > 12000 || b > 12000 {
				textPixel = true
			}
		}
	}
	if !textPixel {
		t.Fatal("text overlay produced no visible pixels")
	}
	black := decoded.At(95, 95)
	if r, g, b, _ := black.RGBA(); r > 3000 || g > 3000 || b > 3000 {
		t.Fatalf("outside pixel=%v", black)
	}
}

func TestCanonicalVectorGeometryPixels(t *testing.T) {
	ffmpeg := toolPath(t, "ffmpeg")
	dir := t.TempDir()
	input := filepath.Join(dir, "input.mp4")
	output := filepath.Join(dir, "output.mp4")
	runTool(t, ffmpeg, "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", "color=c=black:s=160x120:r=30:d=1", "-c:v", "libx264", "-pix_fmt", "yuv420p", input)
	if data, err := os.ReadFile(input); err != nil {
		t.Fatal(err)
	} else if err := os.WriteFile(output, data, 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot := stitch.RenderSnapshot{Document: stitch.Document{Width: 160, Height: 120, Overlays: []stitch.Overlay{
		{ID: "ellipse", Kind: "ellipse", X: 10, Y: 10, Width: 30, Height: 24, Background: "#00ff00", Color: "#00ff00", Opacity: 1, StartUS: 0, EndUS: 1_000_000, Visible: true, Z: 1},
		{ID: "callout", Kind: "callout", X: 55, Y: 8, Width: 40, Height: 28, Text: "A", Background: "#0000ff", Color: "#ffffff", FontSize: 12, Opacity: 1, StartUS: 0, EndUS: 1_000_000, Visible: true, Z: 2},
		{ID: "arrow", Kind: "arrow", X: 105, Y: 12, Width: 35, Height: 30, Color: "#ff0000", Opacity: 1, StartUS: 0, EndUS: 1_000_000, Visible: true, Z: 3},
		{ID: "free", Kind: "freehand", X: 15, Y: 70, Points: []stitch.Point{{X: 15, Y: 70}, {X: 55, Y: 95}, {X: 90, Y: 72}}, Color: "#ffff00", Opacity: 1, StartUS: 0, EndUS: 1_000_000, Visible: true, Z: 4},
	}}}
	if err := applyCanonicalOverlays(context.Background(), output, snapshot); err != nil {
		t.Fatal(err)
	}
	frame := filepath.Join(dir, "frame.png")
	runTool(t, ffmpeg, "-hide_banner", "-loglevel", "error", "-y", "-ss", "0.5", "-i", output, "-frames:v", "1", frame)
	f, err := os.Open(frame)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(f)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	regions := []image.Rectangle{image.Rect(8, 8, 45, 40), image.Rect(52, 5, 100, 45), image.Rect(100, 8, 150, 50), image.Rect(8, 60, 100, 105)}
	for i, region := range regions {
		seen := false
		for y := region.Min.Y; y < region.Max.Y && !seen; y++ {
			for x := region.Min.X; x < region.Max.X; x++ {
				r, g, b, _ := decoded.At(x, y).RGBA()
				if r > 9000 || g > 9000 || b > 9000 {
					seen = true
					break
				}
			}
		}
		if !seen {
			t.Fatalf("vector overlay %d produced no pixels in %v", i, region)
		}
	}
}

func TestCanonicalCaptionBackgroundBoxPixels(t *testing.T) {
	ffmpeg := toolPath(t, "ffmpeg")
	dir := t.TempDir()
	input, output, assPath, frame := filepath.Join(dir, "input.mp4"), filepath.Join(dir, "output.mp4"), filepath.Join(dir, "captions.ass"), filepath.Join(dir, "frame.png")
	runTool(t, ffmpeg, "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", "color=c=black:s=320x180:r=30:d=1", "-c:v", "libx264", "-pix_fmt", "yuv420p", input)
	ass, err := CompileCanonicalCaptions(stitch.Document{Width: 320, Height: 180, Captions: []stitch.Caption{{ID: "c", Text: "BOX", StartUS: 0, EndUS: 1_000_000, Style: stitch.CaptionStyle{Color: "#ffffff", Background: "#ff0000", Font: "Arial", FontSize: 24, X: 160, Y: 130}}}}, "ass", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(assPath, []byte(ass), 0o600); err != nil {
		t.Fatal(err)
	}
	assFile := strings.ReplaceAll(filepath.ToSlash(assPath), ":", `\:`)
	runTool(t, ffmpeg, "-hide_banner", "-loglevel", "error", "-y", "-i", input, "-vf", "ass=filename='"+assFile+"'", "-c:v", "libx264", output)
	runTool(t, ffmpeg, "-hide_banner", "-loglevel", "error", "-y", "-ss", "0.5", "-i", output, "-frames:v", "1", frame)
	f, err := os.Open(frame)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(f)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	red := 0
	for y := 90; y < 150; y++ {
		for x := 80; x < 240; x++ {
			r, g, b, _ := decoded.At(x, y).RGBA()
			if r > 40000 && g < 15000 && b < 15000 {
				red++
			}
		}
	}
	if red < 100 {
		t.Fatalf("caption background produced only %d red pixels", red)
	}
}

func TestCanonicalImageOverlayAudioWebMAndZeroOpacity(t *testing.T) {
	ffmpeg := toolPath(t, "ffmpeg")
	ffprobe := toolPath(t, "ffprobe")
	dir := t.TempDir()
	input := filepath.Join(dir, "input.mp4")
	output := filepath.Join(dir, "output.webm")
	runTool(t, ffmpeg, "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", "color=c=black:s=100x100:r=24:d=2", "-f", "lavfi", "-i", "sine=frequency=440:duration=2", "-c:v", "libx264", "-c:a", "aac", "-shortest", input)
	assetPath := filepath.Join(dir, "red.png")
	img := image.NewNRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 255, A: 255})
		}
	}
	f, err := os.Create(assetPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	data, _ := os.ReadFile(assetPath)
	sum := sha256.Sum256(data)
	asset, _ := json.Marshal(map[string]any{"id": "a", "path": assetPath, "hash": hex.EncodeToString(sum[:]), "size": len(data)})
	snap := stitch.RenderSnapshot{Document: stitch.Document{Width: 100, Height: 100, Overlays: []stitch.Overlay{{ID: "image", Kind: "image", AssetID: "a", X: 10, Y: 10, Width: 20, Height: 20, Opacity: 0, StartUS: 0, EndUS: 2_000_000, Visible: true}}}, Assets: []json.RawMessage{asset}}
	dataIn, _ := os.ReadFile(input)
	if err := os.WriteFile(output, dataIn, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := applyCanonicalOverlays(context.Background(), output, snap); err != nil {
		t.Fatal(err)
	}
	meta := runTool(t, ffprobe, "-v", "error", "-show_entries", "format=format_name,duration:stream=codec_name,codec_type", "-of", "json", output)
	if !strings.Contains(meta, "webm") || !strings.Contains(meta, "vp9") || !strings.Contains(meta, "\"duration\": \"2.") {
		t.Fatalf("webm metadata=%s", meta)
	}
	frame := filepath.Join(dir, "frame.png")
	runTool(t, ffmpeg, "-hide_banner", "-loglevel", "error", "-y", "-ss", "1", "-i", output, "-frames:v", "1", frame)
	f, err = os.Open(frame)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(f)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := decoded.At(15, 15).RGBA()
	if r > 3000 || g > 3000 || b > 3000 {
		t.Fatalf("zero-opacity pixel=%d,%d,%d", r, g, b)
	}
}

func toolPath(t *testing.T, name string) string {
	t.Helper()
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	p := filepath.Join(`C:\bin`, name+".exe")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("%s unavailable: %v", name, err)
	}
	return p
}
func runTool(t *testing.T, path string, args ...string) string {
	t.Helper()
	out, err := exec.Command(path, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", path, err, out)
	}
	return string(out)
}
