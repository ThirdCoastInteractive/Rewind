package ffmpeg

import (
	"context"
	"slices"
	"testing"
)

func TestPrepareArgsLeavesInteractiveFFmpegAlone(t *testing.T) {
	in := []string{"-hide_banner", "-y", "-i", "in.mp4", "out.mp4"}
	got := prepareArgs(context.Background(), in)
	if !slices.Equal(got, in) {
		t.Fatalf("prepareArgs changed interactive ffmpeg: %v", got)
	}
}

func TestPrepareArgsKeepsExplicitHwaccel(t *testing.T) {
	in := []string{"-hwaccel", "cuda", "-i", "in.mp4", "out.mp4"}
	got := prepareArgs(context.Background(), in)
	if !slices.Equal(got, in) {
		t.Fatalf("prepareArgs changed explicit hwaccel: %v", got)
	}
}

func TestPrepareArgsHostShareCapsThreads(t *testing.T) {
	ctx := WithHostShare(context.Background(), 2)
	got := prepareArgs(ctx, []string{"-hide_banner", "-y", "-ss", "1.000", "-i", "in.mp4", "out.jpg"})
	want := []string{"-hide_banner", "-y", "-ss", "1.000", "-hwaccel", "none", "-threads", "2", "-filter_threads", "2", "-i", "in.mp4", "out.jpg"}
	if !slices.Equal(got, want) {
		t.Fatalf("prepareArgs = %v, want %v", got, want)
	}
}

func TestWithHostShareClampsThreads(t *testing.T) {
	ctx := WithHostShare(context.Background(), 99)
	cfg, ok := hostShareFrom(ctx)
	if !ok || cfg.Threads != 8 {
		t.Fatalf("threads = %+v ok=%v", cfg, ok)
	}
	ctx = WithHostShare(context.Background(), 0)
	cfg, ok = hostShareFrom(ctx)
	if !ok || cfg.Threads != 2 {
		t.Fatalf("zero threads = %+v ok=%v", cfg, ok)
	}
}
