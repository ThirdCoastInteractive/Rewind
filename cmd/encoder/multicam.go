package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/ffmpeg"
	"thirdcoast.systems/rewind/pkg/utils/crops"
)

// multicamJobJSON is the JSON envelope stored as the single segment for multicam jobs.
type multicamJobJSON struct {
	Type           string          `json:"type"` // always "multicam"
	VideoID        string          `json:"video_id"`
	Shots          crops.ShotList  `json:"shots"`
	Crops          crops.CropArray `json:"crops"`
	ClipStart      float64         `json:"clip_start"`
	TargetLongEdge int             `json:"target_long_edge,omitempty"` // 0 = default 1920
}

func processMulticam(ctx context.Context, q *db.Queries, exportsDir, downloadsDir string, jobRow *db.FindAndLockPendingStitchJobRow, raw json.RawMessage) error {
	jobID := uuidString(jobRow.ID)
	slog.Info("processing multicam job", "job_id", jobID, "title", jobRow.Title)

	var mcJob multicamJobJSON
	if err := json.Unmarshal(raw, &mcJob); err != nil {
		return fmt.Errorf("failed to parse multicam segment: %w", err)
	}
	if mcJob.VideoID == "" {
		return fmt.Errorf("multicam job missing video_id")
	}
	if len(mcJob.Shots) < 2 {
		return fmt.Errorf("multicam job needs at least 2 shots")
	}
	if len(mcJob.Crops) == 0 {
		return fmt.Errorf("multicam job has no crops")
	}

	// Resolve video file
	videoDir := filepath.Join(downloadsDir, mcJob.VideoID)
	inputPath := findVideoFile(videoDir, mcJob.VideoID)
	if inputPath == "" {
		return fmt.Errorf("video file not found in %s", videoDir)
	}

	// Offset shots by clip start time (shots are clip-relative, ffmpeg needs absolute)
	offsetShots := make(crops.ShotList, len(mcJob.Shots))
	for i, shot := range mcJob.Shots {
		offsetShots[i] = crops.Shot{
			CropID:        shot.CropID,
			Start:         mcJob.ClipStart + shot.Start,
			End:           mcJob.ClipStart + shot.End,
			TransitionOut: shot.TransitionOut,
		}
	}

	// Build crop lookup map
	cropMap := make(map[string]crops.Crop, len(mcJob.Crops))
	for _, cr := range mcJob.Crops {
		cropMap[cr.ID] = cr
	}

	// Create output directory
	stitchExportDir := filepath.Join(exportsDir, "stitch")
	if err := os.MkdirAll(stitchExportDir, 0o755); err != nil {
		return fmt.Errorf("failed to create export dir: %w", err)
	}

	videoPreset, audioPreset, ext := ffmpeg.ExportPresetForFormat(jobRow.Format, jobRow.Quality)
	outputPath := filepath.Join(stitchExportDir, jobID+ext)

	codecOpts := ffmpeg.Flatten(videoPreset)
	if audioPreset != nil {
		codecOpts = append(codecOpts, ffmpeg.Flatten(audioPreset)...)
	}
	codecOpts = append(codecOpts,
		ffmpeg.Metadata("encoded_by", "Rewind Video Archive"),
		ffmpeg.Metadata("comment", "Multicam export from Rewind"),
	)
	if jobRow.Title != "" {
		codecOpts = append(codecOpts, ffmpeg.Metadata("title", jobRow.Title))
	}

	// Probe source video to get its pixel aspect ratio — needed because crop
	// Width/Height are normalized 0-1 fractions, not pixel dimensions.
	probe, err := ffmpeg.Probe(ctx, inputPath)
	if err != nil {
		return fmt.Errorf("failed to probe source: %w", err)
	}
	sourceAspect := 16.0 / 9.0
	if probe.Width > 0 && probe.Height > 0 {
		sourceAspect = float64(probe.Width) / float64(probe.Height)
	}

	outW, outH := ffmpeg.InferMulticamDimensions(offsetShots, cropMap, sourceAspect, mcJob.TargetLongEdge)
	slog.Info("multicam output dimensions", "job_id", jobID, "width", outW, "height", outH, "source_aspect", sourceAspect)

	cmd := ffmpeg.MultiCropCommand(
		inputPath,
		offsetShots,
		cropMap,
		outW, outH,
		outputPath,
		codecOpts...,
	)

	slog.Info("multicam ffmpeg command", "job_id", jobID, "args", strings.Join(cmd.Build(), " "))

	// Estimate total duration for progress tracking
	var totalDur time.Duration
	for _, shot := range mcJob.Shots {
		totalDur += time.Duration((shot.End - shot.Start) * float64(time.Second))
	}

	progressChan := make(chan ffmpeg.Progress, 100)
	proc, err := cmd.StartWithProgress(ctx, progressChan)
	if err != nil {
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	pid := int32(proc.PID())
	_ = q.UpdateStitchJobPID(ctx, &db.UpdateStitchJobPIDParams{
		ID:  jobRow.ID,
		Pid: &pid,
	})

	totalDurMs := totalDur.Milliseconds()
	lastPct := int32(-1)
	lastUpdate := time.Time{}
	for progress := range progressChan {
		if totalDurMs <= 0 {
			continue
		}
		pct := int32((progress.OutTimeMS() * 100) / totalDurMs)
		if pct < 0 {
			pct = 0
		}
		if pct > 99 {
			pct = 99
		}
		now := time.Now()
		if pct != lastPct && now.Sub(lastUpdate) > time.Second {
			lastPct = pct
			lastUpdate = now
			_ = q.UpdateStitchJobProgress(ctx, &db.UpdateStitchJobProgressParams{
				ID:          jobRow.ID,
				ProgressPct: pct,
			})
		}
	}

	if err := proc.Wait(); err != nil {
		_ = os.Remove(outputPath)
		return fmt.Errorf("ffmpeg failed: %w", err)
	}

	st, err := os.Stat(outputPath)
	if err != nil {
		return fmt.Errorf("output file missing after encode: %w", err)
	}

	probe, probeErr := ffmpeg.Probe(ctx, outputPath)
	if probeErr != nil {
		_ = os.Remove(outputPath)
		return fmt.Errorf("output validation failed (ffprobe): %w", probeErr)
	}
	if probe.Duration < 0.5 {
		_ = os.Remove(outputPath)
		return fmt.Errorf("output validation failed: duration too short (%.2fs)", probe.Duration)
	}

	durSec := probe.Duration
	return q.FinishStitchJobReady(ctx, &db.FinishStitchJobReadyParams{
		ID:              jobRow.ID,
		FilePath:        outputPath,
		SizeBytes:       st.Size(),
		DurationSeconds: &durSec,
	})
}
