package main

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"image"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"thirdcoast.systems/rewind/pkg/ffmpeg"
)

const (
	defaultThumbnailLabel = "sm"
)

type thumbnailVariant struct {
	Label    string
	MaxWidth int
}

var thumbnailVariants = []thumbnailVariant{
	{Label: "xs", MaxWidth: 320},
	{Label: "sm", MaxWidth: 640},
	{Label: "md", MaxWidth: 768},
	{Label: "lg", MaxWidth: 1024},
	{Label: "xl", MaxWidth: 1280},
	{Label: "2xl", MaxWidth: 1536},
}

func generateThumbnail(ctx context.Context, videoPath string) (string, error) {
	if strings.TrimSpace(videoPath) == "" {
		return "", errors.New("missing video path")
	}
	videoID := filepath.Base(filepath.Dir(videoPath))
	videoDir := filepath.Dir(videoPath)
	legacy := filepath.Join(videoDir, videoID+".thumbnail.jpg")
	if _, err := os.Stat(legacy); err == nil {
		if ok := thumbnailIsAcceptable(legacy, maxThumbnailWidth()); ok {
			ensureThumbnailVariants(ctx, videoPath, videoID)
			return legacy, nil
		}
	}

	if err := ensureThumbnailVariants(ctx, videoPath, videoID); err != nil {
		return "", err
	}

	defaultPath := thumbnailVariantPath(videoDir, videoID, defaultThumbnailLabel)
	if _, err := os.Stat(defaultPath); err == nil {
		ensureLegacyThumbnailCopy(videoDir, videoID, defaultPath)
		return defaultPath, nil
	}

	return "", fmt.Errorf("thumbnail missing after generation")
}

func generateThumbnailVariant(ctx context.Context, videoPath, out string, maxWidth int) error {
	result := ffmpeg.ExtractThumbnailCapture(ctx, videoPath, out, &ffmpeg.ThumbnailOptions{
		Offset:   5 * time.Second,
		MaxWidth: maxWidth,
		Quality:  4,
	})
	if result.Logs != "" {
		slog.Info("ffmpeg thumbnail output", "output", out, "logs", result.Logs)
	}
	if result.Err != nil {
		_ = os.Remove(out)
		return fmt.Errorf("ffmpeg thumbnail: %w", result.Err)
	}
	return nil
}

func ensureThumbnailVariants(ctx context.Context, videoPath, videoID string) error {
	if strings.TrimSpace(videoID) == "" {
		return errors.New("missing video id")
	}
	videoDir := filepath.Dir(videoPath)
	for _, variant := range thumbnailVariants {
		path := thumbnailVariantPath(videoDir, videoID, variant.Label)
		if _, err := os.Stat(path); err == nil {
			if ok := thumbnailIsAcceptable(path, variant.MaxWidth); ok {
				continue
			}
		}
		if err := generateThumbnailVariant(ctx, videoPath, path, variant.MaxWidth); err != nil {
			return err
		}
	}
	defaultPath := thumbnailVariantPath(videoDir, videoID, defaultThumbnailLabel)
	ensureLegacyThumbnailCopy(videoDir, videoID, defaultPath)
	return nil
}

func thumbnailVariantPath(videoDir, videoID, label string) string {
	return filepath.Join(videoDir, fmt.Sprintf("%s.thumbnail.%s.jpg", videoID, label))
}

func maxThumbnailWidth() int {
	max := 0
	for _, variant := range thumbnailVariants {
		if variant.MaxWidth > max {
			max = variant.MaxWidth
		}
	}
	return max
}

func ensureLegacyThumbnailCopy(videoDir, videoID, srcPath string) {
	legacy := filepath.Join(videoDir, videoID+".thumbnail.jpg")
	if _, err := os.Stat(legacy); err == nil {
		return
	}
	if _, err := os.Stat(srcPath); err != nil {
		return
	}
	if err := os.Link(srcPath, legacy); err == nil {
		return
	}
	if err := copyFile(srcPath, legacy); err != nil {
		slog.Warn("failed to create legacy thumbnail", "path", legacy, "error", err)
	}
}

func thumbnailIsAcceptable(path string, maxWidth int) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return false
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return false
	}
	return cfg.Width <= maxWidth
}

var placeholderGradientLUT = func() [256]byte {
	// Smoothstep-based LUT to keep generated colors in a pleasant mid-range,
	// reducing muddy/dull gradients while staying deterministic.
	// Output range: [32..224].
	var lut [256]byte
	for i := 0; i < 256; i++ {
		t := float64(i) / 255.0
		// smoothstep: 3t^2 - 2t^3
		s := t * t * (3.0 - 2.0*t)
		// Slight contrast boost around midtones.
		s = (s-0.5)*1.15 + 0.5
		if s < 0 {
			s = 0
		}
		if s > 1 {
			s = 1
		}
		v := 32.0 + s*(224.0-32.0)
		lut[i] = byte(math.Round(v))
	}
	return lut
}()

func placeholderGradientForVideoID(videoID string) (start string, end string, angle int32) {
	sum := md5.Sum([]byte(strings.TrimSpace(videoID)))

	max3 := func(a, b, c byte) byte {
		m := a
		if b > m {
			m = b
		}
		if c > m {
			m = c
		}
		return m
	}
	min3 := func(a, b, c byte) byte {
		m := a
		if b < m {
			m = b
		}
		if c < m {
			m = c
		}
		return m
	}
	abs := func(n int) int {
		if n < 0 {
			return -n
		}
		return n
	}

	sr := placeholderGradientLUT[sum[0]]
	sg := placeholderGradientLUT[sum[1]]
	sb := placeholderGradientLUT[sum[2]]
	if int(max3(sr, sg, sb))-int(min3(sr, sg, sb)) < 28 {
		// Avoid low-chroma (gray-ish) starts.
		sr = placeholderGradientLUT[sum[0]^0x55]
		sb = placeholderGradientLUT[sum[2]^0xAA]
	}

	er := placeholderGradientLUT[sum[3]]
	eg := placeholderGradientLUT[sum[4]]
	eb := placeholderGradientLUT[sum[5]]
	if int(max3(er, eg, eb))-int(min3(er, eg, eb)) < 28 {
		// Avoid low-chroma (gray-ish) ends.
		er = placeholderGradientLUT[sum[3]^0xAA]
		eg = placeholderGradientLUT[sum[4]^0x55]
	}

	if abs(int(sr)-int(er)) < 24 && abs(int(sg)-int(eg)) < 24 && abs(int(sb)-int(eb)) < 24 {
		// Ensure start/end are not too similar.
		er = placeholderGradientLUT[sum[3]^0xFF]
		eg = placeholderGradientLUT[sum[4]^0x99]
		eb = placeholderGradientLUT[sum[5]^0x66]
	}

	start = fmt.Sprintf("#%02x%02x%02x", sr, sg, sb)
	end = fmt.Sprintf("#%02x%02x%02x", er, eg, eb)
	angle = 135
	return start, end, angle
}
