package ffmpeg

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStitchLayoutBuildsPortraitModes(t *testing.T) {
	tests := []struct {
		name   string
		layout *Layout
		parts  []string
	}{
		{"single", &Layout{Mode: "single_speaker", Crops: []LayoutCrop{{X: .1, Y: .2, Width: .4, Height: .6}}}, []string{"crop=iw*0.400000:ih*0.600000:iw*0.100000:ih*0.200000", "scale=1080:1920"}},
		{"two", &Layout{Mode: "two_speakers", Crops: []LayoutCrop{{Width: .5, Height: 1}, {X: .5, Width: .5, Height: 1}}}, []string{"split=2", "vstack=inputs=2", "scale=1080:960"}},
		{"preserve", &Layout{Mode: "preserve_scene"}, []string{"boxblur=20:10", "overlay=(W-w)/2:(H-h)/2", "scale=1080:1920"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := StitchCommandFPS([]Segment{{Type: SegmentClip, Input: "input.mp4", Duration: time.Second, HasAudio: true, Layout: tt.layout}}, nil, "out.mp4", nil, nil, 1080, 1920, 30)
			args := strings.Join(cmd.Build(), " ")
			for _, part := range tt.parts {
				if !strings.Contains(args, part) {
					t.Fatalf("layout command missing %q: %s", part, args)
				}
			}
			if StitchCopyEligible([]Segment{{Type: SegmentClip, Input: "input.mp4", Duration: time.Second, HasAudio: true, Layout: tt.layout}}, nil) {
				t.Fatal("layout should force re-encoding")
			}
		})
	}
}

func TestStitchLayoutRendersPortrait(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping ffmpeg layout render")
	}
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not on PATH")
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "source.mp4")
	if output, runErr := exec.Command(ffmpegPath, "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", "testsrc=size=320x180:rate=30", "-t", "0.8", "-pix_fmt", "yuv420p", input).CombinedOutput(); runErr != nil {
		t.Fatalf("create source: %v\n%s", runErr, output)
	}
	for _, layout := range []*Layout{
		{Mode: "single_speaker", Crops: []LayoutCrop{{X: .1, Y: 0, Width: .8, Height: 1}}},
		{Mode: "two_speakers", Crops: []LayoutCrop{{Width: .5, Height: 1}, {X: .5, Width: .5, Height: 1}}},
		{Mode: "preserve_scene"},
	} {
		output := filepath.Join(dir, strings.ReplaceAll(layout.Mode, "_", "-")+".mp4")
		cmd := StitchCommandFPS([]Segment{{Type: SegmentClip, Input: input, Duration: 700 * time.Millisecond, Layout: layout}}, nil, output, nil, nil, 1080, 1920, 30)
		if err := cmd.Run(context.Background()); err != nil {
			t.Fatalf("render %s: %v\n%s", layout.Mode, err, strings.Join(cmd.Build(), " "))
		}
		probe, err := Probe(context.Background(), output)
		if err != nil || probe.Width != 1080 || probe.Height != 1920 {
			t.Fatalf("%s output=%#v err=%v", layout.Mode, probe, err)
		}
	}
}
