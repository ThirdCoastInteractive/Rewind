package frames

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSpriteSelectionAndBounds(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "seek")
	if e := os.MkdirAll(filepath.Join(root, "levels", "fine"), 0755); e != nil {
		t.Fatal(e)
	}
	manifest := `{"format":"rewind-seek-v1","levels":[{"name":"fine","interval_seconds":1,"thumb_width":160,"thumb_height":90,"vtt_path":"levels/fine/seek.vtt"}]}`
	os.WriteFile(filepath.Join(root, "seek.json"), []byte(manifest), 0600)
	vtt := "WEBVTT\n\n00:00:00.000 --> 00:00:01.000\nseek-000.jpg#xywh=0,0,160,90\n\n00:00:01.000 --> 00:00:02.000\nseek-000.jpg#xywh=160,0,160,90\n"
	os.WriteFile(filepath.Join(root, "levels", "fine", "seek.vtt"), []byte(vtt), 0600)
	img := image.NewRGBA(image.Rect(0, 0, 320, 90))
	for y := 0; y < 90; y++ {
		for x := 0; x < 320; x++ {
			c := color.RGBA{R: 255, A: 255}
			if x >= 160 {
				c = color.RGBA{B: 255, A: 255}
			}
			img.Set(x, y, c)
		}
	}
	f, e := os.Create(filepath.Join(root, "levels", "fine", "seek-000.jpg"))
	if e != nil {
		t.Fatal(e)
	}
	jpeg.Encode(f, img, &jpeg.Options{Quality: 100})
	f.Close()
	fs, e := New().Get(context.Background(), Asset{ID: "00000000-0000-0000-0000-000000000001", Directory: dir, Duration: 2}, []float64{0, 1}, "preview", 960)
	if e != nil {
		t.Fatal(e)
	}
	if fs[0].Sample != 0 || fs[1].Sample != 1 || fs[0].Actual != nil || len(fs[1].JPEG) == 0 {
		t.Fatalf("incorrect provenance: %+v", fs)
	}
	if _, e := inside(root, "../../outside"); e == nil {
		t.Fatal("accepted escaped path")
	}
	if _, e := json.Marshal(fs); e != nil {
		t.Fatal(e)
	}
}

func TestPrefersOneSecondFineOverLargerCoarse(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "seek")
	if e := os.MkdirAll(filepath.Join(root, "levels", "coarse"), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.MkdirAll(filepath.Join(root, "levels", "fine"), 0755); e != nil {
		t.Fatal(e)
	}
	manifest := `{"format":"rewind-seek-v1","levels":[{"name":"coarse","interval_seconds":30,"thumb_width":320,"thumb_height":180,"vtt_path":"levels/coarse/seek.vtt"},{"name":"fine","interval_seconds":1,"thumb_width":160,"thumb_height":90,"vtt_path":"levels/fine/seek.vtt"}]}`
	os.WriteFile(filepath.Join(root, "seek.json"), []byte(manifest), 0600)
	os.WriteFile(filepath.Join(root, "levels", "coarse", "seek.vtt"), []byte("WEBVTT\n\n00:00:00.000 --> 00:00:30.000\nseek-000.jpg#xywh=0,0,320,180\n"), 0600)
	os.WriteFile(filepath.Join(root, "levels", "fine", "seek.vtt"), []byte("WEBVTT\n\n00:00:00.000 --> 00:00:01.000\nseek-000.jpg#xywh=0,0,160,90\n"), 0600)
	writeJPEG := func(path string, w, h int, c color.RGBA) {
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				img.Set(x, y, c)
			}
		}
		f, e := os.Create(path)
		if e != nil {
			t.Fatal(e)
		}
		jpeg.Encode(f, img, &jpeg.Options{Quality: 100})
		f.Close()
	}
	writeJPEG(filepath.Join(root, "levels", "coarse", "seek-000.jpg"), 320, 180, color.RGBA{R: 255, A: 255})
	writeJPEG(filepath.Join(root, "levels", "fine", "seek-000.jpg"), 160, 90, color.RGBA{B: 255, A: 255})
	fs, e := New().Get(context.Background(), Asset{ID: "00000000-0000-0000-0000-000000000001", Directory: dir, Duration: 1}, []float64{0}, "preview", 960)
	if e != nil {
		t.Fatal(e)
	}
	if fs[0].Width != 160 || fs[0].Height != 90 {
		t.Fatalf("wanted fine 160x90 tile, got %dx%d", fs[0].Width, fs[0].Height)
	}
}

func TestDetailVFRRotatedOffsetAndSequential(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg unavailable")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "source.mp4")
	path := filepath.Join(dir, "rotated.mp4")
	run := func(args ...string) {
		t.Helper()
		if b, err := exec.Command("ffmpeg", append([]string{"-hide_banner", "-loglevel", "error"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("fixture: %v %s", err, b)
		}
	}
	run("-f", "lavfi", "-i", "testsrc2=size=240x120:rate=10:duration=8", "-vf", "select='if(lt(t,4),1,not(mod(n,3)))',setpts=PTS+5/TB", "-vsync", "0", "-c:v", "mpeg4", "-g", "20", source)
	run("-i", source, "-c", "copy", "-copyts", "-metadata:s:v:0", "rotate=90", path)
	a := Asset{ID: "00000000-0000-0000-0000-000000000001", File: path, Duration: 8}
	s := New()
	frames, err := s.Get(context.Background(), a, []float64{0, 3.15, 5.15}, "detail", 960)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range frames {
		if f.Error != "" {
			t.Fatal(f.Error)
		}
		if f.Width != 120 || f.Height != 240 {
			t.Fatalf("rotation lost: %+v", f)
		}
		if f.Actual == nil || *f.Actual < f.Requested || *f.Actual-f.Requested > .301 {
			t.Fatalf("wrong player PTS: %+v", f)
		}
	}
	samples, err := s.Sample(context.Background(), a, 2, 1, 5)
	if err != nil {
		t.Fatal(err)
	}
	for i, f := range samples {
		if f.Actual == nil || math.Abs(*f.Actual-(2+float64(i))) > .301 || f.Width != 120 || f.Height != 240 {
			t.Fatalf("wrong sequential frame: %+v", f)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = s.Get(ctx, a, []float64{1}, "detail", 960); err == nil {
		t.Fatal("cancelled extraction succeeded")
	}
}
func TestDetailZeroAndPortrait(t *testing.T) {
	if _, e := exec.LookPath("ffmpeg"); e != nil {
		t.Skip("ffmpeg unavailable")
	}
	path := filepath.Join(t.TempDir(), "fixture.mp4")
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=120x240:rate=10:duration=2", "-c:v", "mpeg4", path)
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("fixture: %s %v", b, e)
	}
	fs, e := New().Get(context.Background(), Asset{ID: "00000000-0000-0000-0000-000000000001", File: path, Duration: 2}, []float64{0, 1.15}, "detail", 960)
	if e != nil {
		t.Fatal(e)
	}
	for i, f := range fs {
		if f.Error != "" {
			t.Fatal(f.Error)
		}
		if f.Width != 120 || f.Height != 240 || f.Actual == nil {
			t.Fatalf("bad frame %+v", f)
		}
		if i == 0 && *f.Actual != 0 {
			t.Fatal("timestamp zero moved")
		}
		if i == 1 && (*f.Actual < 1.15 || *f.Actual > 1.21) {
			t.Fatalf("timestamp %v", *f.Actual)
		}
	}
}
