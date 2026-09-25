package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"thirdcoast.systems/rewind/pkg/ffmpeg"
)

const previewAssetDuration = 312.0

type previewAssetLevel struct {
	Name            string  `json:"name"`
	IntervalSeconds float64 `json:"interval_seconds"`
	ThumbWidth      int     `json:"thumb_width"`
	ThumbHeight     int     `json:"thumb_height"`
	Cols            int     `json:"cols"`
	Rows            int     `json:"rows"`
	VTTPath         string  `json:"vtt_path"`
}

type previewSeekManifest struct {
	Format string              `json:"format"`
	Levels []previewAssetLevel `json:"levels"`
}

type previewWaveformManifest struct {
	Format          string  `json:"format"`
	BucketMS        int     `json:"bucket_ms"`
	SampleRateHz    int     `json:"sample_rate_hz"`
	Channels        int     `json:"channels"`
	DurationSeconds float64 `json:"duration_seconds"`
	PeaksPath       string  `json:"peaks_path"`
}

type previewAssets struct {
	Root            string
	Source          string
	GeneratedSource bool
}

func preparePreviewAssets(ctx context.Context, root, mediaPath string) (*previewAssets, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, errors.New("preview assets directory is empty")
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, fmt.Errorf("create preview assets directory: %w", err)
	}
	source := filepath.Join(root, "rewind-live-sample.mp4")
	generated := true
	if strings.TrimSpace(mediaPath) != "" {
		var err error
		source, err = filepath.Abs(mediaPath)
		if err != nil {
			return nil, fmt.Errorf("resolve preview media path: %w", err)
		}
		generated = false
	}
	a := &previewAssets{Root: root, Source: source, GeneratedSource: generated}
	if err := a.ensureSource(ctx); err != nil {
		return a, err
	}
	if err := a.ensureThumbnail(ctx); err != nil {
		return a, err
	}
	if err := a.ensureSeek(ctx); err != nil {
		return a, err
	}
	if err := a.ensureWaveform(ctx); err != nil {
		return a, err
	}
	if err := a.ensureCaptions(); err != nil {
		return a, err
	}
	return a, nil
}

func (a *previewAssets) ensureSource(ctx context.Context) error {
	if !a.GeneratedSource {
		stat, err := os.Stat(a.Source)
		if err != nil || stat.Size() == 0 {
			return fmt.Errorf("configured preview media is unavailable: %s", a.Source)
		}
		return nil
	}
	if stat, err := os.Stat(a.Source); err == nil && stat.Size() > 0 {
		return nil
	}
	// A short, synthetic test pattern keeps the preview self-contained and
	// avoids depending on archived videos, accounts, or network services.
	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=640x360:rate=24",
		"-f", "lavfi", "-i", "aevalsrc=sin(2*PI*440*t)*(0.2+0.8*(0.5+0.5*sin(2*PI*0.01*t))):s=48000",
		"-t", strconv.FormatFloat(previewAssetDuration, 'f', 0, 64),
		"-c:v", "libx264", "-preset", "ultrafast", "-crf", "30",
		"-pix_fmt", "yuv420p", "-c:a", "aac", "-b:a", "64k",
		"-movflags", "+faststart", a.Source,
	}
	if err := runPreviewFFmpeg(ctx, args...); err != nil {
		return fmt.Errorf("generate sample media: %w", err)
	}
	return nil
}

func (a *previewAssets) ensureThumbnail(ctx context.Context) error {
	base := filepath.Join(a.Root, "thumbnail.jpg")
	if stat, err := os.Stat(base); err != nil || stat.Size() == 0 {
		result := ffmpeg.ExtractThumbnail(ctx, a.Source, base, &ffmpeg.ThumbnailOptions{Offset: 5 * time.Second, MaxWidth: 640, Quality: 4})
		if result.Err != nil {
			return fmt.Errorf("generate sample thumbnail: %w", result.Err)
		}
	}
	for _, label := range []string{"xs", "sm", "md", "lg", "xl", "2xl"} {
		path := filepath.Join(a.Root, "thumbnail."+label+".jpg")
		if stat, err := os.Stat(path); err == nil && stat.Size() > 0 {
			continue
		}
		if err := copyPreviewFile(base, path); err != nil {
			return fmt.Errorf("copy sample thumbnail %s: %w", label, err)
		}
	}
	return nil
}

func (a *previewAssets) ensureSeek(ctx context.Context) error {
	seekDir := filepath.Join(a.Root, "seek")
	manifestPath := filepath.Join(seekDir, "seek.json")
	if stat, err := os.Stat(manifestPath); err == nil && stat.Size() > 0 {
		return nil
	}
	levels := []previewAssetLevel{
		{Name: "coarse", IntervalSeconds: 30, ThumbWidth: 96, ThumbHeight: 54, Cols: 12, Rows: 10, VTTPath: "levels/coarse/seek.vtt"},
		{Name: "medium", IntervalSeconds: 10, ThumbWidth: 160, ThumbHeight: 90, Cols: 10, Rows: 10, VTTPath: "levels/medium/seek.vtt"},
		{Name: "fine", IntervalSeconds: 1, ThumbWidth: 160, ThumbHeight: 90, Cols: 10, Rows: 10, VTTPath: "levels/fine/seek.vtt"},
	}
	for _, level := range levels {
		levelDir := filepath.Join(seekDir, "levels", level.Name)
		if err := os.MkdirAll(levelDir, 0755); err != nil {
			return fmt.Errorf("create seek level %s: %w", level.Name, err)
		}
		firstSheet := filepath.Join(levelDir, "seek-000.jpg")
		if stat, err := os.Stat(firstSheet); err != nil || stat.Size() == 0 {
			filter := fmt.Sprintf("fps=1/%g,scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d,tile=%dx%d", level.IntervalSeconds, level.ThumbWidth, level.ThumbHeight, level.ThumbWidth, level.ThumbHeight, level.Cols, level.Rows)
			pattern := filepath.Join(levelDir, "seek-%03d.jpg")
			if err := runPreviewFFmpeg(ctx, "-hide_banner", "-loglevel", "error", "-y", "-i", a.Source, "-vf", filter, "-q:v", "4", "-start_number", "0", pattern); err != nil {
				return fmt.Errorf("generate seek level %s: %w", level.Name, err)
			}
		}
		vttPath := filepath.Join(seekDir, filepath.FromSlash(level.VTTPath))
		if err := writePreviewSeekVTT(vttPath, level, previewAssetDuration); err != nil {
			return fmt.Errorf("write seek VTT %s: %w", level.Name, err)
		}
	}
	b, err := json.MarshalIndent(previewSeekManifest{Format: "rewind-seek-v1", Levels: levels}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(manifestPath, b, 0644)
}

func (a *previewAssets) ensureWaveform(ctx context.Context) error {
	waveformDir := filepath.Join(a.Root, "waveform")
	if err := os.MkdirAll(waveformDir, 0755); err != nil {
		return fmt.Errorf("create waveform directory: %w", err)
	}
	peaksPath := filepath.Join(waveformDir, "peaks.i16")
	manifestPath := filepath.Join(waveformDir, "waveform.json")
	if stat, err := os.Stat(peaksPath); err != nil || stat.Size() == 0 {
		if _, err := ffmpeg.GenerateWaveformPeaks(ctx, a.Source, peaksPath, &ffmpeg.WaveformOptions{SampleRate: 8000, BucketMS: 100}); err != nil {
			return fmt.Errorf("generate waveform peaks: %w", err)
		}
	}
	manifest := previewWaveformManifest{Format: "rewind-waveform-v1", BucketMS: 100, SampleRateHz: 8000, Channels: 1, DurationSeconds: previewAssetDuration, PeaksPath: "peaks.i16"}
	b, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(manifestPath, b, 0644)
}

func (a *previewAssets) ensureCaptions() error {
	path := filepath.Join(a.Root, "captions.vtt")
	if stat, err := os.Stat(path); err == nil && stat.Size() > 0 {
		return nil
	}
	return os.WriteFile(path, []byte("WEBVTT\n\n00:00:00.000 --> 00:00:05.200\nStream everywhere. Archive everything.\n\n00:00:05.200 --> 00:00:12.400\nClip the moments that matter in your browser.\n"), 0644)
}

func (a *previewAssets) thumbnail(label string) string {
	if label == "" {
		label = "sm"
	}
	if !previewThumbnailLabels[label] {
		label = "sm"
	}
	return filepath.Join(a.Root, "thumbnail."+label+".jpg")
}

var previewThumbnailLabels = map[string]bool{"xs": true, "sm": true, "md": true, "lg": true, "xl": true, "2xl": true}

func (a *previewAssets) seekPath(parts ...string) string {
	return filepath.Join(append([]string{a.Root, "seek"}, parts...)...)
}

func (a *previewAssets) waveformPath(name string) string {
	return filepath.Join(a.Root, "waveform", name)
}

func copyPreviewFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func runPreviewFFmpeg(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message != "" {
			return fmt.Errorf("%w: %s", err, message)
		}
		return err
	}
	return nil
}

func writePreviewSeekVTT(path string, level previewAssetLevel, duration float64) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	framesPerSheet := level.Cols * level.Rows
	count := int(duration / level.IntervalSeconds)
	if duration-float64(count)*level.IntervalSeconds > 0.0001 {
		count++
	}
	if count < 1 {
		count = 1
	}
	var b strings.Builder
	b.WriteString("WEBVTT\n\nNOTE rewind-seek-v1\n\n")
	for i := 0; i < count; i++ {
		start := float64(i) * level.IntervalSeconds
		end := start + level.IntervalSeconds
		if end > duration {
			end = duration
		}
		cell := i % framesPerSheet
		fmt.Fprintf(&b, "%s --> %s\nseek-%03d.jpg#xywh=%d,%d,%d,%d\n\n", previewVTTTime(start), previewVTTTime(end), i/framesPerSheet, (cell%level.Cols)*level.ThumbWidth, (cell/level.Cols)*level.ThumbHeight, level.ThumbWidth, level.ThumbHeight)
	}
	return os.WriteFile(path, []byte(b.String()), 0644)
}

func previewVTTTime(seconds float64) string {
	ms := int64(seconds * 1000)
	h := ms / 3600000
	m := (ms / 60000) % 60
	s := (ms / 1000) % 60
	return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, ms%1000)
}
