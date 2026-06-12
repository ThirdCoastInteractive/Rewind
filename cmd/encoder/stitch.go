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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/ffmpeg"
)

// stitchSegmentJSON is the on-the-wire / JSONB format for a sequence segment.
type stitchSegmentJSON struct {
	Type     string  `json:"type"`
	Duration float64 `json:"duration"`
	Title    string  `json:"title,omitempty"`

	// Common media fields (clip, video, stitch)
	StartTs float64             `json:"start_ts,omitempty"`
	EndTs   float64             `json:"end_ts,omitempty"`
	Filters []ffmpeg.FilterSpec `json:"filters,omitempty"`

	// Clip-only
	ClipID  string `json:"clip_id,omitempty"`
	VideoID string `json:"video_id,omitempty"`
	Variant string `json:"variant,omitempty"`

	// Export references (stitch exports)
	ExportJobID string `json:"export_job_id,omitempty"`

	// Title card fields
	BgColor   string `json:"bg_color,omitempty"`
	Text      string `json:"text,omitempty"`
	Subtitle  string `json:"subtitle,omitempty"`
	TextColor string `json:"text_color,omitempty"`
	FontSize  int    `json:"font_size,omitempty"`
	Position  string `json:"position,omitempty"`

	// Transition INTO this segment from the previous one.
	// DataStar coerces null signals to "" so we accept raw JSON and parse manually.
	RawTransition json.RawMessage `json:"transition,omitempty"`

	// Parsed lazily by parseTransition().
	Transition *stitchTransitionJSON `json:"-"`
}

func (s *stitchSegmentJSON) parseTransition() {
	if len(s.RawTransition) > 0 && s.RawTransition[0] == '{' {
		var t stitchTransitionJSON
		if json.Unmarshal(s.RawTransition, &t) == nil && t.Duration > 0 {
			s.Transition = &t
		}
	}
}

type stitchTransitionJSON struct {
	Type     string  `json:"type"`
	Duration float64 `json:"duration"` // seconds
}

// stitchWorker polls for pending stitch jobs and processes them.
// It is run as a goroutine alongside the clip export worker.
func stitchWorker(ctx context.Context, dbc *db.DatabaseConnection, exportsDir, downloadsDir, workerID string, wake <-chan struct{}) {
	q := dbc.Queries(ctx)
	for {
		if ctx.Err() != nil {
			return
		}

		for {
			jobRow, err := q.FindAndLockPendingStitchJob(ctx, &workerID)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					break
				}
				slog.Error("failed to find/lock pending stitch job", "error", err)
				time.Sleep(2 * time.Second)
				break
			}

			if err := processStitch(ctx, q, exportsDir, downloadsDir, jobRow); err != nil {
				jobIDStr := uuidString(jobRow.ID)
				slog.Error("stitch job failed", "job_id", jobIDStr, "error", err)
				errMsg := err.Error()
				_ = q.FinishStitchJobError(ctx, &db.FinishStitchJobErrorParams{
					ID:        jobRow.ID,
					LastError: &errMsg,
				})
				continue
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-wake:
		case <-time.After(5 * time.Second):
		}
	}
}

func processStitch(ctx context.Context, q *db.Queries, exportsDir, downloadsDir string, jobRow *db.FindAndLockPendingStitchJobRow) error {
	jobID := uuidString(jobRow.ID)
	slog.Info("processing stitch job", "job_id", jobID, "title", jobRow.Title, "format", jobRow.Format)

	// Check for multicam job (single segment with type "multicam")
	var rawCheck []json.RawMessage
	if err := json.Unmarshal(jobRow.Segments, &rawCheck); err == nil && len(rawCheck) == 1 {
		var peek struct{ Type string `json:"type"` }
		if json.Unmarshal(rawCheck[0], &peek) == nil && peek.Type == "multicam" {
			return processMulticam(ctx, q, exportsDir, downloadsDir, jobRow, rawCheck[0])
		}
	}

	// Parse segments from JSONB
	var rawSegs []stitchSegmentJSON
	if err := json.Unmarshal(jobRow.Segments, &rawSegs); err != nil {
		return fmt.Errorf("failed to parse segments: %w", err)
	}
	for i := range rawSegs {
		rawSegs[i].parseTransition()
	}
	if len(rawSegs) == 0 {
		return fmt.Errorf("stitch job has no segments")
	}

	// Parse global filters
	var globalFilterSpecs []ffmpeg.FilterSpec
	if len(jobRow.GlobalFilters) > 0 && string(jobRow.GlobalFilters) != "[]" && string(jobRow.GlobalFilters) != "null" {
		if err := json.Unmarshal(jobRow.GlobalFilters, &globalFilterSpecs); err != nil {
			slog.Warn("failed to parse global filters, ignoring", "error", err)
		}
	}

	// Collect clip IDs to bulk-load from DB
	clipIDMap := map[string]*db.GetClipsForStitchRow{}
	var clipPGUUIDs []pgtype.UUID
	for _, seg := range rawSegs {
		if seg.Type == "clip" && seg.ClipID != "" {
			var u pgtype.UUID
			if err := u.Scan(seg.ClipID); err != nil {
				return fmt.Errorf("invalid clip_id %q: %w", seg.ClipID, err)
			}
			clipPGUUIDs = append(clipPGUUIDs, u)
		}
	}

	if len(clipPGUUIDs) > 0 {
		rows, err := q.GetClipsForStitch(ctx, clipPGUUIDs)
		if err != nil {
			return fmt.Errorf("failed to load clips: %w", err)
		}
		for _, r := range rows {
			clipIDMap[uuidString(r.ID)] = r
		}
	}

	// Build ffmpeg.Segment and ffmpeg.Transition slices
	segments := make([]ffmpeg.Segment, 0, len(rawSegs))
	transitions := make([]*ffmpeg.Transition, 0, len(rawSegs))
	var totalDur time.Duration
	hasAudioCache := map[string]bool{}

	probeHasAudio := func(path string) (bool, error) {
		if v, ok := hasAudioCache[path]; ok {
			return v, nil
		}
		probe, err := ffmpeg.Probe(ctx, path)
		if err != nil {
			return false, err
		}
		has := probe.AudioStreams > 0
		hasAudioCache[path] = has
		return has, nil
	}

	for i, raw := range rawSegs {
		// Transition into this segment (nil for first segment or hard cut)
		var tr *ffmpeg.Transition
		if i > 0 && raw.Transition != nil && raw.Transition.Duration > 0 {
			tr = &ffmpeg.Transition{
				Type:     raw.Transition.Type,
				Duration: time.Duration(raw.Transition.Duration * float64(time.Second)),
			}
		}
		transitions = append(transitions, tr)

		switch raw.Type {
		case "title":
			dur := time.Duration(raw.Duration * float64(time.Second))
			if dur <= 0 {
				dur = 3 * time.Second
			}
			segments = append(segments, ffmpeg.Segment{
				Type:          ffmpeg.SegmentTitle,
				TitleDuration: dur,
				BgColor:       raw.BgColor,
				Text:          raw.Text,
				Subtitle:      raw.Subtitle,
				TextColor:     raw.TextColor,
				FontSize:      raw.FontSize,
				Position:      raw.Position,
			})
			totalDur += dur
			if tr != nil {
				totalDur -= tr.Duration
			}

		case "clip":
			clipData, ok := clipIDMap[raw.ClipID]
			if !ok {
				return fmt.Errorf("clip %q not found in database", raw.ClipID)
			}

			// Resolve video file path
			videoID := uuidString(clipData.VideoID)
			videoDir := filepath.Join(downloadsDir, videoID)
			inputPath := findVideoFile(videoDir, videoID)
			if inputPath == "" {
				return fmt.Errorf("video file not found for clip %q in %s", raw.ClipID, videoDir)
			}

			start := time.Duration(clipData.StartTs * float64(time.Second))
			dur := time.Duration(clipData.Duration * float64(time.Second))
			hasAudio, err := probeHasAudio(inputPath)
			if err != nil {
				return fmt.Errorf("failed to probe clip source audio %q: %w", inputPath, err)
			}

			// Compile per-segment filters
			var videoFilters, audioFilters []string
			if len(raw.Filters) > 0 {
				var err error
				videoFilters, audioFilters, err = ffmpeg.CompileFilterStrings(raw.Filters, clipData.Crops)
				if err != nil {
					slog.Warn("failed to compile segment filters, skipping", "clip_id", raw.ClipID, "error", err)
				}
			} else if len(clipData.FilterStack) > 0 && string(clipData.FilterStack) != "[]" && string(clipData.FilterStack) != "null" {
				// Fall back to clip's saved filter stack
				var specs []ffmpeg.FilterSpec
				if err := json.Unmarshal(clipData.FilterStack, &specs); err == nil && len(specs) > 0 {
					var err error
					videoFilters, audioFilters, err = ffmpeg.CompileFilterStrings(specs, clipData.Crops)
					if err != nil {
						slog.Warn("failed to compile clip filter stack, skipping", "clip_id", raw.ClipID, "error", err)
					}
				}
			}

			segments = append(segments, ffmpeg.Segment{
				Type:         ffmpeg.SegmentClip,
				Input:        inputPath,
				Start:        start,
				Duration:     dur,
				HasAudio:     hasAudio,
				VideoFilters: videoFilters,
				AudioFilters: audioFilters,
			})
			totalDur += dur
			if tr != nil {
				totalDur -= tr.Duration
			}

		case "video":
			// Full video — resolve directly by VideoID without clip table lookup.
			if raw.VideoID == "" {
				return fmt.Errorf("segment %d: video segment missing video_id", i)
			}
			videoDir := filepath.Join(downloadsDir, raw.VideoID)
			inputPath := findVideoFile(videoDir, raw.VideoID)
			if inputPath == "" {
				return fmt.Errorf("video file not found for video %q in %s", raw.VideoID, videoDir)
			}

			start := time.Duration(raw.StartTs * float64(time.Second))
			dur := time.Duration(raw.Duration * float64(time.Second))
			hasAudio, err := probeHasAudio(inputPath)
			if err != nil {
				return fmt.Errorf("failed to probe video source audio %q: %w", inputPath, err)
			}
			if raw.EndTs > raw.StartTs {
				dur = time.Duration((raw.EndTs - raw.StartTs) * float64(time.Second))
			}
			if dur <= 0 {
				info, err := ffmpeg.Probe(ctx, inputPath)
				if err != nil {
					return fmt.Errorf("failed to probe video %q: %w", raw.VideoID, err)
				}
				dur = time.Duration(info.Duration*float64(time.Second)) - start
			}

			// Compile per-segment filters (no crops for raw videos)
			var videoFilters, audioFilters []string
			if len(raw.Filters) > 0 {
				var err error
				videoFilters, audioFilters, err = ffmpeg.CompileFilterStrings(raw.Filters, nil)
				if err != nil {
					slog.Warn("failed to compile segment filters, skipping", "video_id", raw.VideoID, "error", err)
				}
			}

			segments = append(segments, ffmpeg.Segment{
				Type:         ffmpeg.SegmentClip,
				Input:        inputPath,
				Start:        start,
				Duration:     dur,
				HasAudio:     hasAudio,
				VideoFilters: videoFilters,
				AudioFilters: audioFilters,
			})
			totalDur += dur
			if tr != nil {
				totalDur -= tr.Duration
			}

		case "stitch":
			// Stitch export — resolve via export job table.
			inputPath, dur, err := resolveExportFile(ctx, q, raw.Type, raw.ExportJobID, raw.Duration, raw.StartTs, raw.EndTs)
			if err != nil {
				return fmt.Errorf("segment %d: %w", i, err)
			}

			start := time.Duration(raw.StartTs * float64(time.Second))
			hasAudio, err := probeHasAudio(inputPath)
			if err != nil {
				return fmt.Errorf("failed to probe %s source audio %q: %w", raw.Type, inputPath, err)
			}

			// Compile per-segment filters
			var videoFilters, audioFilters []string
			if len(raw.Filters) > 0 {
				var compileErr error
				videoFilters, audioFilters, compileErr = ffmpeg.CompileFilterStrings(raw.Filters, nil)
				if compileErr != nil {
					slog.Warn("failed to compile segment filters, skipping", "export_job_id", raw.ExportJobID, "error", compileErr)
				}
			}

			segments = append(segments, ffmpeg.Segment{
				Type:         ffmpeg.SegmentClip,
				Input:        inputPath,
				Start:        start,
				Duration:     dur,
				HasAudio:     hasAudio,
				VideoFilters: videoFilters,
				AudioFilters: audioFilters,
			})
			totalDur += dur
			if tr != nil {
				totalDur -= tr.Duration
			}

		default:
			return fmt.Errorf("unknown segment type: %q", raw.Type)
		}
	}

	// Compile global filters
	var globalVideoFilters, globalAudioFilters []string
	if len(globalFilterSpecs) > 0 {
		var err error
		globalVideoFilters, globalAudioFilters, err = ffmpeg.CompileFilterStrings(globalFilterSpecs, nil)
		if err != nil {
			slog.Warn("failed to compile global filters, ignoring", "error", err)
		}
	}

	// Create output directory
	stitchExportDir := filepath.Join(exportsDir, "stitch")
	if err := os.MkdirAll(stitchExportDir, 0o755); err != nil {
		return fmt.Errorf("failed to create stitch export dir: %w", err)
	}

	// Determine codec presets and extension
	videoPreset, audioPreset, ext := ffmpeg.ExportPresetForFormat(jobRow.Format, jobRow.Quality)
	outputPath := filepath.Join(stitchExportDir, jobID+ext)

	// Build codec opts
	codecOpts := ffmpeg.Flatten(videoPreset)
	if audioPreset != nil {
		codecOpts = append(codecOpts, ffmpeg.Flatten(audioPreset)...)
	}
	codecOpts = append(codecOpts,
		ffmpeg.Metadata("encoded_by", "Rewind Video Archive"),
		ffmpeg.Metadata("comment", "Stitched with Rewind https://github.com/ThirdCoastInteractive/Rewind"),
	)
	if jobRow.Title != "" {
		codecOpts = append(codecOpts, ffmpeg.Metadata("title", jobRow.Title))
	}

	// Build and start stitch command
	cmd := ffmpeg.StitchCommand(
		segments, transitions,
		outputPath,
		globalVideoFilters, globalAudioFilters,
		1920, 1080,
		codecOpts...,
	)

	// Log the full ffmpeg command for debugging
	slog.Info("stitch ffmpeg command", "job_id", jobID, "args", strings.Join(cmd.Build(), " "))

	progressChan := make(chan ffmpeg.Progress, 100)
	proc, err := cmd.StartWithProgress(ctx, progressChan)
	if err != nil {
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	// Store PID
	pid := int32(proc.PID())
	_ = q.UpdateStitchJobPID(ctx, &db.UpdateStitchJobPIDParams{
		ID:  jobRow.ID,
		Pid: &pid,
	})

	// Track progress
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
	if err := q.FinishStitchJobReady(ctx, &db.FinishStitchJobReadyParams{
		ID:              jobRow.ID,
		FilePath:        outputPath,
		SizeBytes:       st.Size(),
		DurationSeconds: &durSec,
	}); err != nil {
		return fmt.Errorf("failed to mark stitch job ready: %w", err)
	}

	slog.Info("stitch job complete", "job_id", jobID, "size_bytes", st.Size())
	return nil
}

// resolveExportFile looks up a completed stitch export job and returns its file path and duration.
func resolveExportFile(ctx context.Context, q *db.Queries, kind string, exportJobID string, rawDuration, startTs, endTs float64) (string, time.Duration, error) {
	if exportJobID == "" {
		return "", 0, fmt.Errorf("%s segment missing export_job_id", kind)
	}

	var u pgtype.UUID
	if err := u.Scan(exportJobID); err != nil {
		return "", 0, fmt.Errorf("invalid export_job_id %q: %w", exportJobID, err)
	}

	var filePath string
	var status db.ExportStatus
	var durSec *float64

	switch kind {
	case "stitch":
		job, err := q.GetStitchExportFile(ctx, u)
		if err != nil {
			return "", 0, fmt.Errorf("stitch job %q not found: %w", exportJobID, err)
		}
		filePath, status, durSec = job.FilePath, job.Status, job.DurationSeconds
	default:
		return "", 0, fmt.Errorf("unknown export kind %q", kind)
	}

	if status != db.ExportStatusReady {
		return "", 0, fmt.Errorf("%s job %q is not ready (status: %s)", kind, exportJobID, status)
	}
	if _, err := os.Stat(filePath); err != nil {
		return "", 0, fmt.Errorf("%s export file missing: %s", kind, filePath)
	}

	// Determine duration: prefer explicit trim, then DB value, then probe.
	dur := time.Duration(rawDuration * float64(time.Second))
	if endTs > startTs {
		dur = time.Duration((endTs - startTs) * float64(time.Second))
	}
	if dur <= 0 && durSec != nil && *durSec > 0 {
		dur = time.Duration(*durSec * float64(time.Second))
	}
	if dur <= 0 {
		info, err := ffmpeg.Probe(ctx, filePath)
		if err != nil {
			return "", 0, fmt.Errorf("failed to probe %s export: %w", kind, err)
		}
		dur = time.Duration(info.Duration * float64(time.Second))
	}

	return filePath, dur, nil
}
