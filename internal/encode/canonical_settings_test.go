package encode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"thirdcoast.systems/rewind/internal/stitch"
)

func TestSliceCanonicalResolvedRangeRebasesSourceAndGaps(t *testing.T) {
	in := []stitch.ResolvedRenderSegment{
		{ID: "a", Type: "video", StartUS: 0, SourceInUS: 10_000_000, DurationUS: 2_000_000},
		{ID: "gap", Type: "gap", StartUS: 2_000_000, DurationUS: 1_000_000},
		{ID: "b", Type: "title", StartUS: 3_000_000, DurationUS: 2_000_000},
	}
	out, err := sliceCanonicalResolved(in, 1_000_000, 4_000_000)
	if err != nil || len(out) != 3 {
		t.Fatalf("range=%#v err=%v", out, err)
	}
	if out[0].StartUS != 0 || out[0].DurationUS != 1_000_000 || out[0].SourceInUS != 11_000_000 {
		t.Fatalf("first=%#v", out[0])
	}
	if out[1].StartUS != 1_000_000 || out[1].DurationUS != 1_000_000 {
		t.Fatalf("gap=%#v", out[1])
	}
	if out[2].StartUS != 2_000_000 || out[2].DurationUS != 1_000_000 {
		t.Fatalf("title=%#v", out[2])
	}
}

func TestRebaseCanonicalDocumentDropsOutsideCaptionsAndPreservesWordGaps(t *testing.T) {
	d := stitch.Document{Captions: []stitch.Caption{
		{ID: "before", Text: "before", StartUS: 2_000_000, EndUS: 4_000_000},
		{ID: "later", Text: "hello world", StartUS: 11_000_000, EndUS: 12_500_000,
			Alignment: "valid", Style: stitch.CaptionStyle{WordHighlight: true},
			Words: []stitch.Word{{Text: "hello", StartUS: 11_200_000, EndUS: 11_500_000}, {Text: "world", StartUS: 12_000_000, EndUS: 12_300_000}}},
		{ID: "after", Text: "after", StartUS: 14_000_000, EndUS: 15_000_000},
	}}
	d = rebaseCanonicalDocument(d, 10_000_000, 13_000_000)
	if len(d.Captions) != 1 || d.Captions[0].ID != "later" || d.Captions[0].StartUS != 1_000_000 || d.Captions[0].EndUS != 2_500_000 {
		t.Fatalf("captions=%#v", d.Captions)
	}
	if len(d.Captions[0].Words) != 2 || d.Captions[0].Words[0].StartUS != 1_200_000 || d.Captions[0].Words[1].EndUS != 2_300_000 {
		t.Fatalf("words=%#v", d.Captions[0].Words)
	}
	ass, err := CompileCanonicalCaptions(d, "ass", false)
	if err != nil || strings.Count(ass, "Dialogue:") < 3 {
		t.Fatalf("ass=%q err=%v", ass, err)
	}
}

func TestRebaseCanonicalDocumentLaterSegmentCaption(t *testing.T) {
	d := stitch.Document{Segments: []stitch.Segment{{ID: "late", Type: "title", StartUS: 10_000_000, DurationUS: 5_000_000}}, Captions: []stitch.Caption{{ID: "cue", SegmentID: "late", Text: "later caption", StartUS: 11_000_000, EndUS: 12_000_000}}}
	d = rebaseCanonicalDocument(d, 10_000_000, 13_000_000)
	if len(d.Segments) != 1 || d.Segments[0].ID != "late" || d.Segments[0].StartUS != 0 || d.Segments[0].DurationUS != 3_000_000 {
		t.Fatalf("segments=%#v", d.Segments)
	}
	srt, err := CompileCanonicalCaptions(d, "srt", false)
	if err != nil || !strings.Contains(srt, "00:00:01,000 --> 00:00:02,000\nlater caption") {
		t.Fatalf("srt=%q err=%v", srt, err)
	}
}

func TestSnapshotFontBytesAreConsumedImmutably(t *testing.T) {
	data := []byte("captured-font-bytes")
	dir, err := writeSnapshotFonts([]stitch.FontReference{{Family: "Fixture Font", Data: data}})
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	got, err := os.ReadFile(filepath.Join(dir, "Fixture_Font.ttf"))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("font=%q err=%v", got, err)
	}
}

func TestSyntheticRangeOutputDimensions(t *testing.T) {
	ffmpeg := fixtureTool(t, "ffmpeg")
	ffprobe := fixtureTool(t, "ffprobe")
	snapshot := stitch.RenderSnapshot{Document: stitch.Document{Width: 160, Height: 90, FPS: 24}, Options: stitch.RenderOptions{Scope: "range", StartUS: 1_000_000, EndUS: 2_500_000}}
	config, err := canonicalRenderConfigFromSnapshot(snapshot)
	if err != nil || config.Width != 160 || config.Height != 90 || config.FPS != 24 || config.EndUS-config.StartUS != 1_500_000 {
		t.Fatalf("config=%#v err=%v", config, err)
	}
	dir := t.TempDir()
	input, output := filepath.Join(dir, "in.mp4"), filepath.Join(dir, "out.mp4")
	run := func(path string, args ...string) []byte {
		out, e := exec.CommandContext(context.Background(), path, args...).CombinedOutput()
		if e != nil {
			t.Fatalf("%s: %v\n%s", path, e, out)
		}
		return out
	}
	run(ffmpeg, "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", "color=c=black:s=320x180:r=24:d=4", "-c:v", "libx264", input)
	// This is the same bounded range and canonical canvas settings consumed by
	// the worker: one and a half seconds, 160x90 at 24fps.
	run(ffmpeg, "-hide_banner", "-loglevel", "error", "-y", "-ss", "1", "-t", "1.5", "-i", input, "-vf", "scale="+fmt.Sprint(config.Width)+":"+fmt.Sprint(config.Height), "-r", fmt.Sprint(config.FPS), output)
	var probe struct {
		Streams []struct {
			Width, Height int
			RFrameRate    string `json:"r_frame_rate"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	raw := run(ffprobe, "-v", "error", "-show_entries", "stream=width,height,r_frame_rate:format=duration", "-of", "json", output)
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatal(err)
	}
	if len(probe.Streams) == 0 || probe.Streams[0].Width != 160 || probe.Streams[0].Height != 90 || probe.Format.Duration < "1.4" || probe.Format.Duration > "1.7" {
		t.Fatalf("probe=%s", raw)
	}
}

func fixtureTool(t *testing.T, name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	p := filepath.Join(`C:\bin`, name+".exe")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("%s unavailable: %v", name, err)
	}
	return p
}
