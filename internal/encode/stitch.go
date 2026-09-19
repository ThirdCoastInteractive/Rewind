package encode

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
	"thirdcoast.systems/rewind/internal/jsnum"
	"thirdcoast.systems/rewind/internal/runtimecfg"
	"thirdcoast.systems/rewind/internal/stitch"
	"thirdcoast.systems/rewind/pkg/audiobeds"
	"thirdcoast.systems/rewind/pkg/ffmpeg"
	"thirdcoast.systems/rewind/pkg/utils/crops"
)

// stitchSegmentJSON is the on-the-wire / JSONB format for a sequence segment.
type stitchSegmentJSON struct {
	Type     string  `json:"type"`
	Duration jsnum.F `json:"duration"`
	Title    string  `json:"title,omitempty"`

	// Common media fields (clip, video, stitch). DataStar may send "".
	StartTs jsnum.F              `json:"start_ts,omitempty"`
	EndTs   jsnum.F              `json:"end_ts,omitempty"`
	Filters []ffmpeg.FilterSpec  `json:"filters,omitempty"`
	Crops   crops.CropArray      `json:"crops,omitempty"`
	Shots   crops.ShotList       `json:"shots,omitempty"`
	Layout  *stitch.TeaserLayout `json:"layout,omitempty"`
	// ClipStartUS is the original clip in-point (µs). Shot times are clip-relative.
	ClipStartUS int64 `json:"clip_start_us,omitempty"`

	// Clip-only
	ClipID  string `json:"clip_id,omitempty"`
	VideoID string `json:"video_id,omitempty"`
	Variant string `json:"variant,omitempty"`

	// Export references (stitch exports)
	ExportJobID string `json:"export_job_id,omitempty"`

	// Title card fields
	BgColor   string  `json:"bg_color,omitempty"`
	Text      string  `json:"text,omitempty"`
	Subtitle  string  `json:"subtitle,omitempty"`
	TextColor string  `json:"text_color,omitempty"`
	FontSize  jsnum.I `json:"font_size,omitempty"`
	Font      string  `json:"font,omitempty"`
	Position  string  `json:"position,omitempty"`
	Audio     string  `json:"audio,omitempty"`
	Role      string  `json:"role,omitempty"`

	// Mix: per-clip trim and look. Match-loudness is a project flag.
	GainDb jsnum.F `json:"gain_db,omitempty"`
	Look   string  `json:"look,omitempty"`

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
	Duration jsnum.F `json:"duration"` // seconds
}

// stitchWorkerID suffixes the base encoder worker id so concurrent stitch
// workers do not share the same locked_by value.
func stitchWorkerID(base string, index int) string {
	return fmt.Sprintf("%s-stitch-%d", base, index)
}

// stitchWorker polls for pending stitch jobs and processes them.
// index is gated by processing.encoder_workers the same way clip exports are.
func stitchWorker(ctx context.Context, dbc *db.DatabaseConnection, exportsDir, downloadsDir, workerID string, wake <-chan struct{}, index int) {
	q := dbc.Queries(ctx)
	for {
		if ctx.Err() != nil {
			return
		}

		for {
			if !runtimecfg.WaitWorker(ctx, "processing.encoder_workers", index) {
				return
			}
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
		case <-time.After(idlePoll):
		}
	}
}

func processStitch(ctx context.Context, q *db.Queries, exportsDir, downloadsDir string, jobRow *db.FindAndLockPendingStitchJobRow) error {
	if jobRow.RenderKind == "frame" {
		return processCanonicalFrame(ctx, q, exportsDir, downloadsDir, jobRow)
	}
	if len(jobRow.DocumentSnapshot) > 0 {
		return processCanonicalStitch(ctx, q, exportsDir, downloadsDir, jobRow)
	}
	return processStitchBody(ctx, q, exportsDir, downloadsDir, jobRow)
}

func processStitchBody(ctx context.Context, q *db.Queries, exportsDir, downloadsDir string, jobRow *db.FindAndLockPendingStitchJobRow) error {
	return processStitchBodyWithPost(ctx, q, exportsDir, downloadsDir, jobRow, nil)
}

func processStitchBodyWithPost(ctx context.Context, q *db.Queries, exportsDir, downloadsDir string, jobRow *db.FindAndLockPendingStitchJobRow, post func(string) error) error {
	return processStitchBodyWithPostFinalize(ctx, q, exportsDir, downloadsDir, jobRow, post, true)
}

func processStitchBodyWithPostFinalize(ctx context.Context, q *db.Queries, exportsDir, downloadsDir string, jobRow *db.FindAndLockPendingStitchJobRow, post func(string) error, finalize bool) error {
	canonicalJob := len(jobRow.DocumentSnapshot) > 0
	jobID := uuidString(jobRow.ID)
	slog.Info("processing stitch job", "job_id", jobID, "title", jobRow.Title, "format", jobRow.Format)

	var rawCheck []json.RawMessage
	if err := json.Unmarshal(jobRow.Segments, &rawCheck); err == nil && len(rawCheck) == 1 {
		var peek struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(rawCheck[0], &peek) == nil && peek.Type == "multicam" {
			return processMulticam(ctx, q, exportsDir, downloadsDir, jobRow, rawCheck[0])
		}
	}

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

	var globalFilterSpecs []ffmpeg.FilterSpec
	if len(jobRow.GlobalFilters) > 0 && string(jobRow.GlobalFilters) != "[]" && string(jobRow.GlobalFilters) != "null" {
		if err := json.Unmarshal(jobRow.GlobalFilters, &globalFilterSpecs); err != nil {
			if canonicalJob {
				return fmt.Errorf("invalid canonical document filters: %w", err)
			}
			slog.Warn("failed to parse global filters, ignoring", "error", err)
		}
	}
	matchLoudness := ffmpeg.MatchLoudnessEnabled(globalFilterSpecs)
	loudnessTarget := ffmpeg.MatchLoudnessTarget(globalFilterSpecs)
	burnCaptions := ffmpeg.BurnCaptionsEnabled(globalFilterSpecs)
	captionFont := ffmpeg.BurnCaptionsFont(globalFilterSpecs)
	globalFilterSpecs = ffmpeg.StripControlFilters(globalFilterSpecs)

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
		var tr *ffmpeg.Transition
		if i > 0 && raw.Transition != nil && raw.Transition.Duration > 0 {
			tr = &ffmpeg.Transition{
				Type:     raw.Transition.Type,
				Duration: time.Duration(float64(raw.Transition.Duration) * float64(time.Second)),
			}
		}
		transitions = append(transitions, tr)

		switch raw.Type {
		case "title":
			dur := time.Duration(float64(raw.Duration) * float64(time.Second))
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
				Font:          raw.Font,
				FontSize:      int(raw.FontSize),
				Position:      raw.Position,
				Audio:         resolveTitleAudio(raw.Audio),
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

			var videoFilters, audioFilters []string
			if len(raw.Filters) > 0 {
				var err error
				cropData := clipData.Crops
				if raw.Crops != nil {
					cropData = raw.Crops
				}
				videoFilters, audioFilters, err = ffmpeg.CompileFilterStrings(raw.Filters, cropData)
				if err != nil {
					slog.Warn("failed to compile segment filters, skipping", "clip_id", raw.ClipID, "error", err)
				}
			} else if len(clipData.FilterStack) > 0 && string(clipData.FilterStack) != "[]" && string(clipData.FilterStack) != "null" {
				var specs []ffmpeg.FilterSpec
				if err := json.Unmarshal(clipData.FilterStack, &specs); err == nil && len(specs) > 0 {
					var err error
					cropData := clipData.Crops
					if raw.Crops != nil {
						cropData = raw.Crops
					}
					videoFilters, audioFilters, err = ffmpeg.CompileFilterStrings(specs, cropData)
					if err != nil {
						slog.Warn("failed to compile clip filter stack, skipping", "clip_id", raw.ClipID, "error", err)
					}
				}
			}
			videoFilters, audioFilters = ffmpeg.AppendStitchMix(videoFilters, audioFilters, raw.Look, float64(raw.GainDb), matchLoudness, raw.Filters)
			videoFilters = appendBurnCaptions(videoFilters, burnCaptions, captionFont, videoDir, videoID, start, dur)

			clipStartUS := raw.ClipStartUS
			if clipStartUS == 0 {
				clipStartUS = int64(clipData.StartTs * 1e6)
			}
			mcRaw := raw
			mcRaw.ClipStartUS = clipStartUS
			if len(mcRaw.Crops) == 0 {
				mcRaw.Crops = clipData.Crops
			}
			if len(mcRaw.Shots) == 0 {
				mcRaw.Shots = clipData.ShotList
			}
			segCrops, segShots, segLayout := encoderMulticam(mcRaw, start.Seconds(), dur.Seconds())

			segments = append(segments, ffmpeg.Segment{
				Type:         ffmpeg.SegmentClip,
				Input:        inputPath,
				Start:        start,
				Duration:     dur,
				HasAudio:     hasAudio,
				VideoFilters: videoFilters,
				AudioFilters: audioFilters,
				Layout:       segLayout,
				Crops:        segCrops,
				Shots:        segShots,
			})
			totalDur += dur
			if tr != nil {
				totalDur -= tr.Duration
			}

		case "video":
			if raw.VideoID == "" {
				return fmt.Errorf("segment %d: video segment missing video_id", i)
			}
			videoDir := filepath.Join(downloadsDir, raw.VideoID)
			inputPath := findVideoFile(videoDir, raw.VideoID)
			if inputPath == "" {
				return fmt.Errorf("video file not found for video %q in %s", raw.VideoID, videoDir)
			}

			start := time.Duration(float64(raw.StartTs) * float64(time.Second))
			dur := time.Duration(float64(raw.Duration) * float64(time.Second))
			hasAudio, err := probeHasAudio(inputPath)
			if err != nil {
				return fmt.Errorf("failed to probe video source audio %q: %w", inputPath, err)
			}
			if raw.EndTs > raw.StartTs {
				dur = time.Duration(float64(raw.EndTs-raw.StartTs) * float64(time.Second))
			}
			if dur <= 0 {
				info, err := ffmpeg.Probe(ctx, inputPath)
				if err != nil {
					return fmt.Errorf("failed to probe video %q: %w", raw.VideoID, err)
				}
				dur = time.Duration(info.Duration*float64(time.Second)) - start
			}

			var videoFilters, audioFilters []string
			if len(raw.Filters) > 0 {
				var err error
				videoFilters, audioFilters, err = ffmpeg.CompileFilterStrings(raw.Filters, raw.Crops)
				if err != nil {
					slog.Warn("failed to compile segment filters, skipping", "video_id", raw.VideoID, "error", err)
				}
			}
			videoFilters, audioFilters = ffmpeg.AppendStitchMix(videoFilters, audioFilters, raw.Look, float64(raw.GainDb), matchLoudness, raw.Filters)
			videoFilters = appendBurnCaptions(videoFilters, burnCaptions, captionFont, videoDir, raw.VideoID, start, dur)

			segCrops, segShots, segLayout := encoderMulticam(raw, start.Seconds(), dur.Seconds())

			segments = append(segments, ffmpeg.Segment{
				Type:         ffmpeg.SegmentClip,
				Input:        inputPath,
				Start:        start,
				Duration:     dur,
				HasAudio:     hasAudio,
				VideoFilters: videoFilters,
				AudioFilters: audioFilters,
				Layout:       segLayout,
				Crops:        segCrops,
				Shots:        segShots,
			})
			totalDur += dur
			if tr != nil {
				totalDur -= tr.Duration
			}

		case "stitch":
			inputPath, dur, err := resolveExportFile(ctx, q, raw.Type, raw.ExportJobID, float64(raw.Duration), float64(raw.StartTs), float64(raw.EndTs))
			if err != nil {
				return fmt.Errorf("segment %d: %w", i, err)
			}

			start := time.Duration(float64(raw.StartTs) * float64(time.Second))
			hasAudio, err := probeHasAudio(inputPath)
			if err != nil {
				return fmt.Errorf("failed to probe %s source audio %q: %w", raw.Type, inputPath, err)
			}

			var videoFilters, audioFilters []string
			if len(raw.Filters) > 0 {
				var compileErr error
				videoFilters, audioFilters, compileErr = ffmpeg.CompileFilterStrings(raw.Filters, nil)
				if compileErr != nil {
					slog.Warn("failed to compile segment filters, skipping", "export_job_id", raw.ExportJobID, "error", compileErr)
				}
			}
			videoFilters, audioFilters = ffmpeg.AppendStitchMix(videoFilters, audioFilters, raw.Look, float64(raw.GainDb), matchLoudness, raw.Filters)

			segments = append(segments, ffmpeg.Segment{
				Type:         ffmpeg.SegmentClip,
				Input:        inputPath,
				Start:        start,
				Duration:     dur,
				HasAudio:     hasAudio,
				VideoFilters: videoFilters,
				AudioFilters: audioFilters,
				Layout:       encoderLayout(raw.Layout),
			})
			totalDur += dur
			if tr != nil {
				totalDur -= tr.Duration
			}

		default:
			return fmt.Errorf("unknown segment type: %q", raw.Type)
		}
	}

	var globalVideoFilters, globalAudioFilters []string
	if len(globalFilterSpecs) > 0 {
		var err error
		globalVideoFilters, globalAudioFilters, err = ffmpeg.CompileFilterStrings(globalFilterSpecs, nil)
		if err != nil {
			if canonicalJob {
				return fmt.Errorf("invalid canonical global filter: %w", err)
			}
			slog.Warn("failed to compile global filters, ignoring", "error", err)
		}
	}

	stitchExportDir := filepath.Join(exportsDir, "stitch")
	if err := os.MkdirAll(stitchExportDir, 0o755); err != nil {
		return fmt.Errorf("failed to create stitch export dir: %w", err)
	}

	videoPreset, audioPreset, ext := ffmpeg.ExportPresetForFormat(jobRow.Format, jobRow.Quality)
	outputPath := filepath.Join(stitchExportDir, jobID+ext)

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

	outW, outH := 1920, 1080
	outFPS := 30.0
	canonicalDimensions := false
	if len(jobRow.DocumentSnapshot) > 0 {
		var canonical stitch.RenderSnapshot
		if err := json.Unmarshal(jobRow.DocumentSnapshot, &canonical); err == nil {
			if config, configErr := canonicalRenderConfigFromSnapshot(canonical); configErr == nil {
				outW, outH, outFPS = config.Width, config.Height, config.FPS
				canonicalDimensions = true
			}
		}
	}
	var srcProbe *ffmpeg.ProbeResult
	for _, seg := range segments {
		if seg.Type != ffmpeg.SegmentClip || seg.Input == "" {
			continue
		}
		p, err := ffmpeg.Probe(ctx, seg.Input)
		if err == nil && p != nil {
			srcProbe = p
			if !canonicalDimensions && p.Width > 0 && p.Height > 0 {
				outW, outH = ffmpeg.FitExportSize(p.Width, p.Height, 1920, 1080)
			}
			if !canonicalDimensions && p.FPS >= 1 && p.FPS <= 120 {
				outFPS = p.FPS
			}
		}
		break
	}

	if matchLoudness {
		for i := range segments {
			seg := &segments[i]
			if seg.Type != ffmpeg.SegmentClip || seg.Input == "" || seg.Duration <= 0 {
				continue
			}
			m, err := ffmpeg.MeasureLoudnorm(ctx, seg.Input, seg.Start, seg.Duration, loudnessTarget)
			if err != nil {
				slog.Warn("loudnorm measure failed, single-pass", "job_id", jobID, "error", err)
				seg.AudioFilters = ffmpeg.ReplaceLoudnorm(seg.AudioFilters, loudnessTarget, nil)
				continue
			}
			slog.Info("loudnorm measure", "job_id", jobID, "input_i", m.InputI, "target", loudnessTarget)
			seg.AudioFilters = ffmpeg.ReplaceLoudnorm(seg.AudioFilters, loudnessTarget, m)
		}
	}

	copied := false
	if srcProbe != nil && (jobRow.Format == "mp4" || jobRow.Format == "") &&
		len(globalVideoFilters) == 0 && len(globalAudioFilters) == 0 &&
		ffmpeg.StitchCopyEligible(segments, transitions) {
		slog.Info("stitch copy-concat", "job_id", jobID, "w", outW, "h", outH, "fps", outFPS)
		if err := ffmpeg.StitchCopyConcat(ctx, segments, outputPath, srcProbe, nil); err != nil {
			slog.Warn("stitch copy-concat failed, re-encoding", "job_id", jobID, "error", err)
			_ = os.Remove(outputPath)
		} else {
			copied = true
		}
	}

	if !copied {
		stitchCommand := ffmpeg.StitchCommandFPS
		if canonicalJob {
			stitchCommand = ffmpeg.StitchCommandFPSStrict
		}
		cmd := stitchCommand(
			segments, transitions,
			outputPath,
			globalVideoFilters, globalAudioFilters,
			outW, outH, outFPS,
			codecOpts...,
		)
		slog.Info("stitch ffmpeg command", "job_id", jobID, "args", strings.Join(cmd.Build(), " "))

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
	if probe.VideoStreams < 1 {
		_ = os.Remove(outputPath)
		return fmt.Errorf("output validation failed: no video stream")
	}
	if probe.Duration < 0.5 {
		_ = os.Remove(outputPath)
		return fmt.Errorf("output validation failed: duration too short (%.2fs)", probe.Duration)
	}
	if post != nil {
		if err := post(outputPath); err != nil {
			return fmt.Errorf("canonical composition failed: %w", err)
		}
		st, err = os.Stat(outputPath)
		if err != nil {
			return fmt.Errorf("output missing after composition: %w", err)
		}
	}
	if !finalize {
		return nil
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

func processCanonicalStitch(ctx context.Context, q *db.Queries, exportsDir, downloadsDir string, jobRow *db.FindAndLockPendingStitchJobRow) error {
	return processCanonicalStitchFinalize(ctx, q, exportsDir, downloadsDir, jobRow, true, nil)
}

func processCanonicalStitchFinalize(ctx context.Context, q *db.Queries, exportsDir, downloadsDir string, jobRow *db.FindAndLockPendingStitchJobRow, finalize bool, extraPost func(string) error) error {
	var snap stitch.RenderSnapshot
	if err := json.Unmarshal(jobRow.DocumentSnapshot, &snap); err != nil {
		return fmt.Errorf("invalid canonical snapshot: %w", err)
	}
	var err error
	if snap.Options.Scope == "range" {
		snap.Resolved, err = sliceCanonicalResolved(snap.Resolved, snap.Options.StartUS, snap.Options.EndUS)
		if err != nil {
			return err
		}
		snap.Document = rebaseCanonicalDocument(snap.Document, snap.Options.StartUS, snap.Options.EndUS)
	}
	segs, err := canonicalLegacySegments(snap)
	if err != nil {
		return err
	}
	b, _ := json.Marshal(segs)
	jobCopy := *jobRow
	jobCopy.Segments = b
	jobCopy.GlobalFilters, _ = json.Marshal(snap.Document.Settings.Filter)
	if len(jobCopy.GlobalFilters) == 0 || string(jobCopy.GlobalFilters) == "null" {
		jobCopy.GlobalFilters = []byte("[]")
	}
	if snap.Options.LoudnessTarget != 0 {
		var filters []ffmpeg.FilterSpec
		if err := json.Unmarshal(jobCopy.GlobalFilters, &filters); err != nil {
			return fmt.Errorf("invalid document filters: %w", err)
		}
		filters = append(filters, ffmpeg.FilterSpec{Type: "match_loudness", Params: map[string]any{"target": snap.Options.LoudnessTarget}})
		jobCopy.GlobalFilters, _ = json.Marshal(filters)
	}
	post := func(output string) error { return applyCanonicalOverlays(ctx, output, snap) }
	if snap.Options.CaptionMode == "none" || len(snap.Document.Captions) == 0 {
		return processStitchBodyWithPostFinalize(ctx, q, exportsDir, downloadsDir, &jobCopy, combinePosts(post, extraPost), finalize)
	}
	format := snap.Options.CaptionMode
	if format == "burn" {
		format = "ass"
	}
	content, err := CompileCanonicalCaptions(snap.Document, format, true)
	if err != nil {
		return err
	}
	if snap.Options.CaptionMode == "burn" {
		f, err := os.CreateTemp("", "rewind-stitch-captions-*.ass")
		if err != nil {
			return err
		}
		name := f.Name()
		defer os.Remove(name)
		if _, err = f.WriteString(content); err != nil {
			f.Close()
			return err
		}
		if err = f.Close(); err != nil {
			return err
		}
		fontDir, err := writeSnapshotFonts(snap.Fonts)
		if err != nil {
			return err
		}
		defer os.RemoveAll(fontDir)
		var globals []ffmpeg.FilterSpec
		if len(jobCopy.GlobalFilters) > 0 {
			_ = json.Unmarshal(jobCopy.GlobalFilters, &globals)
		}
		assName := strings.ReplaceAll(filepath.ToSlash(name), ":", `\:`)
		assName = strings.ReplaceAll(assName, "'", `\'`)
		fontsPath := strings.ReplaceAll(filepath.ToSlash(fontDir), ":", `\:`)
		fontsPath = strings.ReplaceAll(fontsPath, "'", `\'`)
		globals = append(globals, ffmpeg.FilterSpec{Type: "ass", Params: map[string]any{"filename": assName, "fontsdir": fontsPath}})
		jobCopy.GlobalFilters, _ = json.Marshal(globals)
		return processStitchBodyWithPostFinalize(ctx, q, exportsDir, downloadsDir, &jobCopy, combinePosts(post, extraPost), finalize)
	}
	return processStitchBodyWithPostFinalize(ctx, q, exportsDir, downloadsDir, &jobCopy, func(output string) error {
		if err := post(output); err != nil {
			return err
		}
		if extraPost != nil {
			if err := extraPost(output); err != nil {
				return err
			}
		}
		ext := "." + snap.Options.CaptionMode
		return os.WriteFile(output+ext, []byte(content), 0o644)
	}, finalize)
}

func combinePosts(a, b func(string) error) func(string) error {
	return func(path string) error {
		if a != nil {
			if err := a(path); err != nil {
				return err
			}
		}
		if b != nil {
			return b(path)
		}
		return nil
	}
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

func appendBurnCaptions(videoFilters []string, enabled bool, family, videoDir, videoID string, start, dur time.Duration) []string {
	if !enabled || videoID == "" || dur <= 0 {
		return videoFilters
	}
	f, err := ffmpeg.BurnCaptionsFilter(videoDir, videoID, start.Seconds(), dur.Seconds(), family)
	if err != nil {
		slog.Warn("stitch burn captions skipped", "video_id", videoID, "error", err)
		return videoFilters
	}
	if f == "" {
		return videoFilters
	}
	return append(videoFilters, f)
}

func resolveTitleAudio(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if p := audiobeds.File(id); p != "" {
		return p
	}
	return ""
}
