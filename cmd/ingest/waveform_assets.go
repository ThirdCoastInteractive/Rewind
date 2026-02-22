package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"thirdcoast.systems/rewind/pkg/ffmpeg"
)

type waveformManifest struct {
	Format          string  `json:"format"`
	BucketMS        int     `json:"bucket_ms"`
	SampleRateHz    int     `json:"sample_rate_hz"`
	Channels        int     `json:"channels"`
	DurationSeconds float64 `json:"duration_seconds"`
	PeaksPath       string  `json:"peaks_path"`
}

func waveformDirForVideoPath(videoPath string) (string, error) {
	videoPath = strings.TrimSpace(videoPath)
	if videoPath == "" {
		return "", errors.New("missing video path")
	}
	return filepath.Join(filepath.Dir(videoPath), "waveform"), nil
}

func ensureWaveformAssets(ctx context.Context, videoPath string, durationSeconds *int32) (bool, error) {
	wfDir, err := waveformDirForVideoPath(videoPath)
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(wfDir, 0755); err != nil {
		return false, fmt.Errorf("mkdir waveform dir: %w", err)
	}

	// Check for no-audio marker first - if video has no audio, skip silently
	noAudioMarker := filepath.Join(wfDir, ".no-audio")
	if _, err := os.Stat(noAudioMarker); err == nil {
		return false, nil
	}

	manifestPath := filepath.Join(wfDir, "waveform.json")
	peaksPath := filepath.Join(wfDir, "peaks.i16")

	bucketMS := 100
	sampleRate := 8000
	channels := 1

	regen := false
	if b, err := os.ReadFile(manifestPath); err != nil {
		regen = true
	} else {
		var m waveformManifest
		if err := json.Unmarshal(b, &m); err != nil {
			regen = true
		} else if m.Format != waveformFormatV1 || m.BucketMS != bucketMS || m.SampleRateHz != sampleRate || m.Channels != channels || strings.TrimSpace(m.PeaksPath) == "" {
			regen = true
		} else if _, err := os.Stat(peaksPath); err != nil {
			regen = true
		}
	}

	if !regen {
		return false, nil
	}

	// Probe video to check for audio track before attempting generation
	probeResult, err := ffmpeg.Probe(ctx, videoPath)
	if err != nil {
		return false, fmt.Errorf("probe failed: %w", err)
	}

	if probeResult.AudioCodec == "" || probeResult.AudioCodec == "none" {
		// Video has no audio - create marker so we don't keep trying
		markerContent := fmt.Sprintf("Video has no audio track\nProbed: %s\nVideo codec: %s\n", videoPath, probeResult.VideoCodec)
		if err := os.WriteFile(noAudioMarker, []byte(markerContent), 0644); err != nil {
			return false, fmt.Errorf("write no-audio marker: %w", err)
		}
		return false, nil
	}

	dur, err := resolveDurationSeconds(ctx, videoPath, durationSeconds)
	if err != nil {
		return false, err
	}

	if err := generateWaveformPeaks(ctx, videoPath, peaksPath, bucketMS, sampleRate); err != nil {
		return true, err
	}

	m := waveformManifest{
		Format:          waveformFormatV1,
		BucketMS:        bucketMS,
		SampleRateHz:    sampleRate,
		Channels:        channels,
		DurationSeconds: dur,
		PeaksPath:       "peaks.i16",
	}
	mb, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return true, err
	}
	if err := os.WriteFile(manifestPath, mb, 0644); err != nil {
		return true, err
	}

	return true, nil
}

func generateWaveformPeaks(ctx context.Context, videoPath, outPath string, bucketMS, sampleRate int) error {
	if bucketMS <= 0 {
		return errors.New("invalid bucket_ms")
	}
	if sampleRate <= 0 {
		return errors.New("invalid sample rate")
	}

	result, err := ffmpeg.GenerateWaveformPeaks(ctx, videoPath, outPath, &ffmpeg.WaveformOptions{
		SampleRate: sampleRate,
		BucketMS:   bucketMS,
	})
	if err != nil {
		_ = os.Remove(outPath)
		return fmt.Errorf("ffmpeg waveform: %w", err)
	}
	if result.Logs != "" {
		slog.Info("ffmpeg waveform output", "logs", result.Logs)
	}

	// Small delay to reduce IO bursts (preserved from original)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(10 * time.Millisecond):
	}

	_ = result // Result contains PeakCount and Duration if needed later
	return nil
}
