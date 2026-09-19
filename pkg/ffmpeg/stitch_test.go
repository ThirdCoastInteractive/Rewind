package ffmpeg

import (
	"strings"
	"testing"
	"time"

	"thirdcoast.systems/rewind/pkg/utils/crops"
)

func TestMulticamChainsTwoShotsHasSplitCropXfade(t *testing.T) {
	cropA := crops.Crop{ID: "camA", X: 0.25, Y: 0.5, Width: 0.4, Height: 0.8}
	cropB := crops.Crop{ID: "camB", X: 0.75, Y: 0.5, Width: 0.4, Height: 0.8}
	shots := crops.ShotList{
		{CropID: "camA", Start: 0, End: 1.0, TransitionOut: &crops.ShotTransition{Type: "fade", Duration: 0.2}},
		{CropID: "camB", Start: 1.0, End: 2.0},
	}
	cmd := StitchCommandFPS([]Segment{{
		Type:     SegmentClip,
		Input:    "input.mp4",
		Duration: 2 * time.Second,
		HasAudio: true,
		Crops:    crops.CropArray{cropA, cropB},
		Shots:    shots,
	}}, nil, "out.mp4", nil, nil, 1920, 1080, 30)
	args := strings.Join(cmd.Build(), " ")
	for _, part := range []string{"split=", "crop=", "xfade"} {
		if !strings.Contains(args, part) {
			t.Fatalf("multicam filter_complex missing %q: %s", part, args)
		}
	}
	if !strings.Contains(args, "force_original_aspect_ratio=increase") {
		t.Fatalf("expected punch-in fill scale: %s", args)
	}
	if StitchCopyEligible([]Segment{{
		Type: SegmentClip, Input: "input.mp4", Duration: time.Second, HasAudio: true,
		Shots: shots, Crops: crops.CropArray{cropA, cropB},
	}}, nil) {
		t.Fatal("multicam should force re-encoding")
	}
}

func TestMulticamChainsMissingCropFallsBack(t *testing.T) {
	cmd := StitchCommandFPS([]Segment{{
		Type:     SegmentClip,
		Input:    "input.mp4",
		Duration: time.Second,
		HasAudio: true,
		Crops:    crops.CropArray{{ID: "other", X: 0.5, Y: 0.5, Width: 0.5, Height: 0.5}},
		Shots:    crops.ShotList{{CropID: "missing", Start: 0, End: 1}},
	}}, nil, "out.mp4", nil, nil, 1920, 1080, 30)
	args := strings.Join(cmd.Build(), " ")
	if strings.Contains(args, "split=") || strings.Contains(args, "xfade") {
		t.Fatalf("expected full-frame fallback, got multicam: %s", args)
	}
}

func TestMulticamWinsOverLayout(t *testing.T) {
	cmd := StitchCommandFPS([]Segment{{
		Type:     SegmentClip,
		Input:    "input.mp4",
		Duration: 2 * time.Second,
		HasAudio: true,
		Layout:   &Layout{Mode: "preserve_scene"},
		Crops:    crops.CropArray{{ID: "a", X: 0.3, Y: 0.5, Width: 0.4, Height: 0.7}, {ID: "b", X: 0.7, Y: 0.5, Width: 0.4, Height: 0.7}},
		Shots: crops.ShotList{
			{CropID: "a", Start: 0, End: 1},
			{CropID: "b", Start: 1, End: 2},
		},
	}}, nil, "out.mp4", nil, nil, 1080, 1920, 30)
	args := strings.Join(cmd.Build(), " ")
	if strings.Contains(args, "boxblur") || strings.Contains(args, "vstack") {
		t.Fatalf("layout should not apply when shots present: %s", args)
	}
	if !strings.Contains(args, "split=") || !strings.Contains(args, "xfade") {
		t.Fatalf("expected multicam chains: %s", args)
	}
}
