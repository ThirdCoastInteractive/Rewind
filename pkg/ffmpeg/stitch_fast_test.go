package ffmpeg

import (
	"strings"
	"testing"
	"time"
)

func TestSplitChapterTitle(t *testing.T) {
	t.Parallel()
	k, m := splitChapterTitle("Chapter 1: Staying Positive")
	if k != "Chapter 1" || m != "Staying Positive" {
		t.Fatalf("got %q / %q", k, m)
	}
	k, m = splitChapterTitle("Just a title")
	if k != "" || m != "Just a title" {
		t.Fatalf("got %q / %q", k, m)
	}
}

func TestStitchHardCutsUseConcat(t *testing.T) {
	t.Parallel()
	cmd := StitchCommand(
		[]Segment{
			{Type: SegmentTitle, TitleDuration: 3 * time.Second, Text: "Chapter 1: Staying Positive", Subtitle: "Ben Avery"},
			{Type: SegmentClip, Input: "in.mp4", Duration: 60 * time.Second, HasAudio: true},
		},
		[]*Transition{nil, nil},
		"out.mp4",
		nil, nil,
		1280, 720,
	)
	joined := strings.Join(cmd.Build(), " ")
	if !strings.Contains(joined, "concat=n=2:v=1:a=1") {
		t.Fatalf("expected concat hard cut, got %s", joined)
	}
	if strings.Contains(joined, "xfade=") {
		t.Fatalf("hard cuts should not xfade: %s", joined)
	}
	if !strings.Contains(joined, "flags=lanczos") {
		t.Fatalf("expected lanczos scale: %s", joined)
	}
}

func TestTitleCardX264Args(t *testing.T) {
	t.Parallel()
	args := titleCardX264Args(Segment{
		Type:          SegmentTitle,
		TitleDuration: 4 * time.Second,
		Text:          "Chapter 1: Brave",
		Subtitle:      "Ben Avery",
	}, "title.mp4", 1920, 1080, 30, 48000)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "libx264") {
		t.Fatalf("expected libx264: %s", joined)
	}
	if strings.Contains(joined, "stillimage") {
		t.Fatalf("stillimage drops title cards in concat: %s", joined)
	}
	if strings.Contains(joined, "ultrafast") {
		t.Fatalf("title cards should not use ultrafast: %s", joined)
	}
	if !strings.Contains(joined, "-bf 0") {
		t.Fatalf("expected no B-frames for concat: %s", joined)
	}
	if !strings.Contains(joined, "force-cfr=1") {
		t.Fatalf("expected CFR: %s", joined)
	}
}

func TestTitleCardUsesAudioBed(t *testing.T) {
	t.Parallel()
	args := titleCardX264Args(Segment{
		Type:          SegmentTitle,
		TitleDuration: 6 * time.Second,
		Text:          "Chapter 1: Brave",
		Audio:         "/audio/dies-irae-open.ogg",
	}, "title.mp4", 1920, 1080, 30, 48000)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "/audio/dies-irae-open.ogg") {
		t.Fatalf("expected sting path: %s", joined)
	}
	if !strings.Contains(joined, "afade=") {
		t.Fatalf("expected fade: %s", joined)
	}
	if strings.Contains(joined, "anullsrc") {
		t.Fatalf("bed should replace silence: %s", joined)
	}
}

func TestStitchCopyEligible(t *testing.T) {
	t.Parallel()
	segs := []Segment{
		{Type: SegmentTitle, TitleDuration: 4 * time.Second, Text: "Chapter 1: Staying Positive"},
		{Type: SegmentClip, Input: "a.mp4", Duration: 20 * time.Minute, HasAudio: true},
	}
	if !StitchCopyEligible(segs, []*Transition{nil, nil}) {
		t.Fatal("expected eligible")
	}
	segs[1].AudioFilters = []string{"loudnorm=I=-14.0:TP=-1.5:LRA=11.0"}
	if !StitchCopyEligible(segs, []*Transition{nil, nil}) {
		t.Fatal("audio-only loudnorm should still copy video")
	}
	args := copyClipMPEGTSArgs(segs[1], "p.ts", 48000)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-c:v copy") || !strings.Contains(joined, "-af ") {
		t.Fatalf("expected copy video + encode audio: %s", joined)
	}
	segs[1].VideoFilters = []string{"eq=contrast=1.1"}
	if StitchCopyEligible(segs, []*Transition{nil, nil}) {
		t.Fatal("video filters should block copy")
	}
}

func TestFitExportSizeNoUpscale(t *testing.T) {
	t.Parallel()
	w, h := FitExportSize(1280, 720, 1920, 1080)
	if w != 1280 || h != 720 {
		t.Fatalf("got %dx%d", w, h)
	}
	w, h = FitExportSize(3840, 2160, 1920, 1080)
	if w != 1920 || h != 1080 {
		t.Fatalf("got %dx%d", w, h)
	}
}
