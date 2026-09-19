package ffmpeg

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// StitchCopyEligible is true when every join is a hard cut, clips have no
// extra filters, and we can stream-copy the long parts after a short title.
func StitchCopyEligible(segments []Segment, transitions []*Transition) bool {
	if len(segments) == 0 {
		return false
	}
	if !stitchHardCuts(transitions, len(segments)) {
		return false
	}
	clips := 0
	for _, s := range segments {
		switch s.Type {
		case SegmentTitle:
			if s.TitleDuration <= 0 {
				return false
			}
		case SegmentClip:
			if s.Input == "" || s.Duration <= 0 || !s.HasAudio || s.Layout != nil || len(s.Shots) > 0 {
				return false
			}
			if len(s.VideoFilters) > 0 {
				return false
			}
			clips++
		default:
			return false
		}
	}
	return clips >= 1
}

// StitchCopyConcat writes output by stream-copying clip ranges and encoding
// only title cards, then mpegts-concat. Falls back to error for the caller
// to use StitchCommandFPS.
func StitchCopyConcat(ctx context.Context, segments []Segment, output string, probe *ProbeResult, progress chan<- Progress) error {
	if probe == nil {
		return fmt.Errorf("probe required")
	}
	if !strings.EqualFold(probe.VideoCodec, "h264") {
		return fmt.Errorf("copy concat needs h264, have %s", probe.VideoCodec)
	}
	w, h := FitExportSize(probe.Width, probe.Height, 1920, 1080)
	fps := probe.FPS
	if fps <= 0 || fps > 120 {
		fps = 30
	}
	ar := probe.AudioSampleRate
	if ar <= 0 {
		ar = 48000
	}

	dir, err := os.MkdirTemp("", "rewind-stitch-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	parts := make([]string, 0, len(segments))
	for i, seg := range segments {
		part := filepath.Join(dir, fmt.Sprintf("p%d.ts", i))
		switch seg.Type {
		case SegmentTitle:
			if err := encodeTitleMPEGTS(ctx, seg, part, w, h, fps, ar); err != nil {
				return err
			}
		case SegmentClip:
			if err := copyClipMPEGTS(ctx, seg, part, ar); err != nil {
				return err
			}
		}
		parts = append(parts, part)
	}

	listPath := filepath.Join(dir, "list.txt")
	var b strings.Builder
	for _, p := range parts {
		fmt.Fprintf(&b, "file '%s'\n", strings.ReplaceAll(filepath.ToSlash(p), `'`, `'\''`))
	}
	if err := os.WriteFile(listPath, []byte(b.String()), 0o644); err != nil {
		return err
	}

	args := []string{
		"-hide_banner", "-y",
		"-f", "concat", "-safe", "0", "-i", listPath,
		"-c", "copy",
		"-bsf:a", "aac_adtstoasc",
		"-movflags", "+faststart",
		output,
	}
	cmd := &Command{rawArgs: args, output: output}
	if progress != nil {
		return cmd.RunWithProgress(ctx, progress)
	}
	return cmd.Run(ctx)
}

func encodeTitleMPEGTS(ctx context.Context, seg Segment, out string, w, h int, fps float64, ar int) error {
	mp4 := strings.TrimSuffix(out, filepath.Ext(out)) + ".mp4"
	if err := (&Command{rawArgs: titleCardX264Args(seg, mp4, w, h, fps, ar)}).Run(ctx); err != nil {
		return err
	}
	// Remux the encoded title into mpegts so concat copy can join it with clips.
	return (&Command{rawArgs: []string{
		"-hide_banner", "-y",
		"-i", mp4,
		"-c", "copy",
		"-bsf:v", "h264_mp4toannexb",
		"-muxdelay", "0", "-muxpreload", "0",
		"-f", "mpegts",
		out,
	}}).Run(ctx)
}

// titleCardX264Args encodes a title card as real CFR libx264 (never stillimage /
// copy). stillimage + mpegts concat drops the card in players.
func titleCardX264Args(seg Segment, out string, w, h int, fps float64, ar int) []string {
	bg := seg.BgColor
	if bg == "" {
		bg = "black"
	}
	fg := seg.TextColor
	if fg == "" {
		fg = "white"
	}
	if fps <= 0 || fps > 120 {
		fps = 30
	}
	if ar <= 0 {
		ar = 48000
	}
	gop := int(fps + 0.5)
	if gop < 1 {
		gop = 30
	}
	dur := seg.TitleDuration.Seconds()
	if dur <= 0 {
		dur = 3
	}
	lavfi := titleCardLavfi(seg, bg, fg, w, h, fps, dur)
	args := []string{
		"-hide_banner", "-y",
		"-f", "lavfi", "-i", lavfi,
	}
	args = append(args, titleAudioInput(seg, dur, ar)...)
	args = append(args,
		"-r", fmt.Sprintf("%g", fps),
		"-fps_mode", "cfr",
		"-c:v", "libx264", "-preset", "medium", "-crf", "18",
		"-pix_fmt", "yuv420p", "-profile:v", "high", "-level", "4.1",
		"-bf", "0",
		"-g", fmt.Sprintf("%d", gop),
		"-x264-params", fmt.Sprintf("keyint=%d:min-keyint=%d:scenecut=0:force-cfr=1", gop, gop),
		"-color_primaries", "bt709", "-color_trc", "bt709", "-colorspace", "bt709",
		"-af", titleAudioFilters(dur),
		"-c:a", "aac", "-ar", fmt.Sprintf("%d", ar), "-ac", "2", "-b:a", "192k",
		"-t", fmt.Sprintf("%.6f", dur),
		"-shortest",
		"-movflags", "+faststart",
		out,
	)
	return args
}

func titleAudioInput(seg Segment, dur float64, ar int) []string {
	if p := strings.TrimSpace(seg.Audio); p != "" {
		return []string{"-stream_loop", "-1", "-i", p}
	}
	if ar <= 0 {
		ar = 48000
	}
	if dur <= 0 {
		dur = 3
	}
	return []string{"-f", "lavfi", "-t", fmt.Sprintf("%.6f", dur), "-i", fmt.Sprintf("anullsrc=r=%d:cl=stereo", ar)}
}

func titleAudioFilters(dur float64) string {
	if dur <= 0 {
		dur = 3
	}
	fadeIn := 0.35
	fadeOut := 0.85
	if dur < 1.4 {
		fadeIn, fadeOut = 0.08, 0.18
	}
	if fadeOut > dur/2 {
		fadeOut = dur / 2
	}
	st := dur - fadeOut
	if st < 0 {
		st = 0
	}
	return fmt.Sprintf("atrim=0:%.6f,afade=t=in:st=0:d=%.3f,afade=t=out:st=%.6f:d=%.3f,aformat=sample_fmts=fltp:channel_layouts=stereo,asetpts=PTS-STARTPTS", dur, fadeIn, st, fadeOut)
}

func copyClipMPEGTS(ctx context.Context, seg Segment, out string, ar int) error {
	return (&Command{rawArgs: copyClipMPEGTSArgs(seg, out, ar)}).Run(ctx)
}

func copyClipMPEGTSArgs(seg Segment, out string, ar int) []string {
	args := []string{
		"-hide_banner", "-y",
		"-ss", formatDuration(seg.Start),
		"-t", formatDuration(seg.Duration),
		"-i", seg.Input,
		"-c:v", "copy",
		"-bsf:v", "h264_mp4toannexb",
	}
	if len(seg.AudioFilters) > 0 {
		if ar <= 0 {
			ar = 48000
		}
		args = append(args,
			"-af", strings.Join(seg.AudioFilters, ","),
			"-c:a", "aac", "-ar", fmt.Sprintf("%d", ar), "-ac", "2", "-b:a", "192k",
		)
	} else {
		args = append(args, "-c:a", "copy")
	}
	return append(args, "-f", "mpegts", out)
}
