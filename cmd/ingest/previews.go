package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"thirdcoast.systems/rewind/pkg/ffmpeg"
)

func generatePreviewMP4(ctx context.Context, videoPath string) error {
	if strings.TrimSpace(videoPath) == "" {
		return errors.New("missing video path")
	}
	videoID := filepath.Base(filepath.Dir(videoPath))
	out := filepath.Join(filepath.Dir(videoPath), videoID+".preview.mp4")
	if _, err := os.Stat(out); err == nil {
		return nil
	}

	result := ffmpeg.GeneratePreviewCapture(ctx, videoPath, out, &ffmpeg.PreviewOptions{
		StartOffset: 10 * time.Second,
		Duration:    6 * time.Second,
		MaxWidth:    480,
	})
	if result.Logs != "" {
		slog.Info("ffmpeg preview output", "video_id", videoID, "logs", result.Logs)
	}
	if result.Err != nil {
		_ = os.Remove(out)
		return fmt.Errorf("ffmpeg preview: %w", result.Err)
	}

	if _, err := os.Stat(out); err != nil {
		return fmt.Errorf("preview missing after ffmpeg")
	}
	return nil
}
