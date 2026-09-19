package ffmpeg

import (
	"strings"
	"testing"
	"time"
)

func TestFFmpegColorStripsHash(t *testing.T) {
	t.Parallel()
	if got := ffmpegColor("#000000"); got != "0x000000" {
		t.Fatalf("got %q", got)
	}
	if got := ffmpegColor("#f5f0e6"); got != "0xF5F0E6" {
		t.Fatalf("got %q", got)
	}
	if got := ffmpegColor("#fff"); got != "0xFFFFFF" {
		t.Fatalf("got %q", got)
	}
	if got := ffmpegColor("white"); got != "white" {
		t.Fatalf("got %q", got)
	}
	if got := ffmpegColor("#ffffff@0.55"); got != "0xFFFFFF@0.55" {
		t.Fatalf("got %q", got)
	}
}

func TestTitleCardLavfiHasNoHash(t *testing.T) {
	t.Parallel()
	lavfi := titleCardLavfi(Segment{
		Type:          SegmentTitle,
		TitleDuration: 3 * time.Second,
		BgColor:       "#000000",
		Text:          "Chapter 4: Brave",
		Subtitle:      "Ben Avery",
		TextColor:     "#f5f0e6",
		FontSize:      56,
	}, "#000000", "#f5f0e6", 1920, 1080, 30, 3)
	if strings.Contains(lavfi, "#") {
		t.Fatalf("hash comments out the rest of the filtergraph: %s", lavfi)
	}
	if !strings.Contains(lavfi, "0x000000") {
		t.Fatalf("expected 0x background: %s", lavfi)
	}
	if !strings.Contains(lavfi, "0xF5F0E6") {
		t.Fatalf("expected 0x text: %s", lavfi)
	}
}
