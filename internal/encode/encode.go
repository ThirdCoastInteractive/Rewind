package encode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"thirdcoast.systems/rewind/internal/config"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/runtimecfg"
	"thirdcoast.systems/rewind/pkg/ffmpeg"
	"thirdcoast.systems/rewind/pkg/plugin"
	"thirdcoast.systems/rewind/pkg/utils/crops"
)

const idlePoll = 30 * time.Second

var videoExtensions = []string{".webm", ".mp4", ".mkv", ".mov", ".avi"}

// Start runs encoder and stitch workers until ctx is cancelled.
func Start(ctx context.Context, dbc *db.DatabaseConnection, conf *config.Config) error {
	_ = conf

	if _, _, err := ffmpeg.TitleFontPaths(); err != nil {
		return fmt.Errorf("failed to extract title-card fonts: %w", err)
	}
	if err := runtimecfg.Start(ctx, dbc, "encoder"); err != nil {
		return fmt.Errorf("live settings initialization failed: %w", err)
	}

	exportsDir := strings.TrimSpace(os.Getenv("EXPORTS_DIR"))
	if exportsDir == "" {
		exportsDir = "/exports"
	}
	if err := os.MkdirAll(filepath.Join(exportsDir, "clips"), 0o755); err != nil {
		return fmt.Errorf("failed to create exports dir %s: %w", exportsDir, err)
	}

	downloadsDir, ok := plugin.LocalRoot()
	if !ok {
		downloadsDir = strings.TrimSpace(os.Getenv("DOWNLOADS_DIR"))
		if downloadsDir == "" {
			downloadsDir = "/downloads"
		}
	}

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = fmt.Sprintf("pid-%d", os.Getpid())
	}
	workerID := fmt.Sprintf("encoder-%s", hostname)

	slog.Info("Recovering stuck exports from previous service instances")
	if err := dbc.Queries(ctx).ResetStuckExports(ctx); err != nil {
		slog.Error("failed to recover stuck exports", "error", err)
	}
	// This process just started; any processing stitch row is orphaned from a prior instance.
	if err := dbc.Queries(ctx).RequeueAllProcessingStitchJobs(ctx); err != nil {
		slog.Error("failed to recover processing stitch jobs", "error", err)
	}
	cleanupMissingExportFiles(ctx, dbc)

	wake := make(chan struct{}, 1)
	stitchWake := make(chan struct{}, 1)
	signal := func(ch chan<- struct{}) {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	go db.RunListenLoop(ctx, dbc, []string{"clip_exports", "stitch_jobs"}, func(n *pgconn.Notification) {
		switch n.Channel {
		case "clip_exports":
			signal(wake)
		case "stitch_jobs":
			signal(stitchWake)
		}
	}, func() {
		signal(wake)
		signal(stitchWake)
	})

	workers := runtimecfg.Int(ctx, "processing.encoder_workers")
	if workers < 1 {
		workers = 1
	}
	slog.Info("Encoder workers started", "workers", workers, "worker_id", workerID)
	for i := 0; i < 32; i++ {
		go encoderWorker(ctx, dbc, exportsDir, downloadsDir, workerID, wake, i)
		go stitchWorker(ctx, dbc, exportsDir, downloadsDir, stitchWorkerID(workerID, i), stitchWake, i)
	}
	go func() {
		t := time.NewTicker(2 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				// Stale-only: live encodes refresh updated_at via UpdateStitchJobProgress.
				if err := dbc.Queries(ctx).ResetStuckStitchJobs(ctx); err != nil {
					slog.Error("reset stuck stitch jobs", "error", err)
				}
				if err := dbc.Queries(ctx).ResetStuckExports(ctx); err != nil {
					slog.Error("reset stuck clip exports", "error", err)
				}
			}
		}
	}()

	<-ctx.Done()
	slog.Info("Encoder service stopping")
	return nil
}

func encoderWorker(ctx context.Context, dbc *db.DatabaseConnection, exportsDir, downloadsDir, workerID string, wake <-chan struct{}, index int) {
	q := dbc.Queries(ctx)
	for {
		if ctx.Err() != nil {
			return
		}

		for {
			if !runtimecfg.WaitWorker(ctx, "processing.encoder_workers", index) {
				return
			}
			exportRow, err := q.FindAndLockPendingClipExport(ctx, &workerID)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					break
				}
				slog.Error("failed to find/lock pending export", "error", err)
				time.Sleep(2 * time.Second)
				break
			}

			jobCtx, captureErr := runtimecfg.Job(ctx, dbc, "export", exportRow.ID)
			if captureErr != nil {
				message := captureErr.Error()
				_ = q.FinishClipExportError(ctx, &db.FinishClipExportErrorParams{ID: exportRow.ID, LastError: &message})
				continue
			}
			if err := processExport(jobCtx, q, exportsDir, downloadsDir, exportRow); err != nil {
				exportID := uuidString(exportRow.ID)
				slog.Error("export failed", "export_id", exportID, "error", err)
				errMsg := err.Error()
				_ = q.FinishClipExportError(ctx, &db.FinishClipExportErrorParams{
					ID:        exportRow.ID,
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

func processExport(ctx context.Context, q *db.Queries, exportsDir, downloadsDir string, exportRow *db.FindAndLockPendingClipExportRow) error {
	exportID := uuidString(exportRow.ID)
	clipID := uuidString(exportRow.ClipID)

	slog.Info("processing export", "export_id", exportID, "clip_id", clipID, "variant", exportRow.Variant)

	clipData, err := q.GetClipForExport(ctx, exportRow.ClipID)
	if err != nil {
		return fmt.Errorf("failed to get clip data: %w", err)
	}

	videoID := uuidString(clipData.VideoID)
	if clipData.TenantID.Valid && clipData.TenantID.Bytes != [16]byte{} {
		if incoming, scoped := plugin.TenantScope(ctx); scoped && incoming != clipData.TenantID.String() {
			return fmt.Errorf("clip export tenant scope mismatch")
		}
		ctx = plugin.WithTenantScope(ctx, clipData.TenantID.String(), true)
	} else if plugin.LiveIngest() != nil {
		return fmt.Errorf("live clip export requires a workspace tenant")
	}
	var inputPath string
	var cleanup func()
	var srcErr error
	if clipData.VideoPath != nil && strings.TrimSpace(*clipData.VideoPath) != "" {
		inputPath, cleanup, srcErr = plugin.MasterSourceAt(ctx, *clipData.VideoPath)
	}
	if srcErr != nil && plugin.LiveIngest() != nil {
		return fmt.Errorf("resolve workspace master for %s: %w", videoID, srcErr)
	}
	if inputPath == "" {
		inputPath, cleanup, srcErr = plugin.MasterSource(ctx, videoID)
	}
	if srcErr != nil || inputPath == "" {
		videoDir := filepath.Join(downloadsDir, videoID)
		inputPath = findVideoFile(videoDir, videoID)
		if inputPath == "" {
			if srcErr != nil {
				return fmt.Errorf("video file not found for %s: %w", videoID, srcErr)
			}
			return fmt.Errorf("video file not found for %s", videoID)
		}
		cleanup = func() {}
	}
	defer cleanup()

	clipExportDir := filepath.Join(exportsDir, "clips", clipID)
	if err := os.MkdirAll(clipExportDir, 0o755); err != nil {
		return fmt.Errorf("failed to create export dir: %w", err)
	}

	var specQuality string
	if len(exportRow.Spec) > 0 {
		var specPeek struct {
			Quality string `json:"quality"`
		}
		_ = json.Unmarshal(exportRow.Spec, &specPeek)
		specQuality = specPeek.Quality
	}
	videoPreset, audioPreset, ext := ffmpeg.ExportPresetForFormat(exportRow.Format, specQuality)
	outputPath := filepath.Join(clipExportDir, exportID+ext)

	if err := q.UpdateClipExportFilePath(ctx, &db.UpdateClipExportFilePathParams{
		ID:       exportRow.ID,
		FilePath: outputPath,
	}); err != nil {
		slog.Warn("failed to update export file path", "error", err)
	}

	start := time.Duration(clipData.StartTs * float64(time.Second))
	end := time.Duration((clipData.StartTs + clipData.Duration) * float64(time.Second))

	opts := ffmpeg.Flatten(videoPreset)
	if audioPreset != nil {
		opts = append(opts, ffmpeg.Flatten(audioPreset)...)
	}
	opts = append(opts,
		ffmpeg.Metadata("encoded_by", "Rewind Video Archive"),
		ffmpeg.Metadata("comment", "Exported with Rewind https://github.com/ThirdCoastInteractive/Rewind"),
	)

	if clipData.ClipTitle != "" {
		opts = append(opts, ffmpeg.Metadata("title", clipData.ClipTitle))
	}

	if len(clipData.FilterStack) > 0 && string(clipData.FilterStack) != "[]" && string(clipData.FilterStack) != "null" {
		opts = append(opts, ffmpeg.Metadata("rewind_filter_stack", string(clipData.FilterStack)))
	}

	if strings.HasPrefix(exportRow.Variant, "crop:") {
		cropID := strings.TrimPrefix(exportRow.Variant, "crop:")
		for _, cr := range clipData.Crops {
			if cr.ID == cropID {
				cropMeta := cr.Name
				if cr.AspectRatio != "" {
					cropMeta += " (" + cr.AspectRatio + ")"
				}
				opts = append(opts, ffmpeg.Metadata("rewind_crop", cropMeta))
				break
			}
		}
	}

	var specApplied bool
	var exportFilters []ffmpeg.FilterSpec
	if len(exportRow.Spec) > 0 {
		var spec ffmpeg.ExportSpec
		if err := json.Unmarshal(exportRow.Spec, &spec); err != nil {
			slog.Warn("failed to parse export spec, falling back to variant", "error", err)
		} else if len(spec.Filters) > 0 {
			exportFilters = spec.Filters
			filterOpts, filterErr := ffmpeg.CompileFilters(spec.Filters, clipData.Crops)
			if filterErr != nil {
				slog.Warn("failed to compile filter spec, falling back to variant", "error", filterErr)
			} else {
				opts = append(opts, filterOpts...)
				specApplied = true
			}
		}
	}

	if !specApplied {
		variant := exportRow.Variant
		if strings.HasPrefix(variant, "crop:") {
			cropID := strings.TrimPrefix(variant, "crop:")
			if filter := crops.BuildCropFilterByID(clipData.Crops, cropID); filter != "" {
				opts = append(opts, ffmpeg.Filter(filter))
				opts = append(opts, ffmpeg.EvenDimensions())
			}
		}
	}

	progressChan := make(chan ffmpeg.Progress, 100)
	allOpts := append([]ffmpeg.Option{ffmpeg.SeekTo(start, end)}, opts...)
	cmd := ffmpeg.NewCommand(inputPath, outputPath, allOpts...)
	if target, ok := clipLoudnormTarget(exportFilters, clipData.FilterStack); ok {
		m, mErr := ffmpeg.MeasureLoudnorm(ctx, inputPath, start, time.Duration(clipData.Duration*float64(time.Second)), target)
		if mErr != nil {
			slog.Warn("clip loudnorm measure failed, single-pass", "error", mErr)
			cmd.SetMeasuredLoudnorm(target, nil)
		} else {
			slog.Info("clip loudnorm measure", "input_i", m.InputI, "target", target)
			cmd.SetMeasuredLoudnorm(target, m)
		}
	}

	proc, err := cmd.StartWithProgress(ctx, progressChan)
	if err != nil {
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	pid := int32(proc.PID())
	if err := q.UpdateClipExportPID(ctx, &db.UpdateClipExportPIDParams{
		ID:  exportRow.ID,
		Pid: &pid,
	}); err != nil {
		slog.Warn("failed to store ffmpeg PID", "error", err, "pid", pid)
	}

	lastPct := -1
	lastUpdate := time.Time{}
	for progress := range progressChan {
		if clipData.Duration <= 0 {
			continue
		}
		pct := int((float64(progress.OutTimeMS()) / (clipData.Duration * 1000)) * 100)
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
			_ = q.UpdateClipExportProgress(ctx, &db.UpdateClipExportProgressParams{
				ID:          exportRow.ID,
				ProgressPct: int32(pct),
			})
		}
	}

	if err := proc.Wait(); err != nil {
		_ = os.Remove(outputPath)
		return fmt.Errorf("ffmpeg failed: %w", err)
	}

	st, err := os.Stat(outputPath)
	if err != nil {
		return fmt.Errorf("output file missing: %w", err)
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

	if err := q.FinishClipExportReady(ctx, &db.FinishClipExportReadyParams{
		ID:        exportRow.ID,
		FilePath:  outputPath,
		SizeBytes: st.Size(),
	}); err != nil {
		return fmt.Errorf("failed to mark export ready: %w", err)
	}

	if b := plugin.Blobs(); b != nil {
		key := plugin.VideoKey(videoID, "exports/"+filepath.Base(outputPath))
		if _, local := b.LocalPath(key); !local {
			if err := uploadExportBlob(ctx, b, key, outputPath); err != nil {
				slog.Warn("failed to upload export blob", "key", key, "error", err)
			}
		}
	}

	slog.Info("export complete", "export_id", exportID, "clip_id", clipID, "size_bytes", st.Size())
	return nil
}

func uploadExportBlob(ctx context.Context, b plugin.Blob, key, path string) error {
	w, err := b.Create(ctx, key)
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		_ = w.Close()
		return err
	}
	_, copyErr := io.Copy(w, f)
	closeFileErr := f.Close()
	closeBlobErr := w.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeFileErr != nil {
		return closeFileErr
	}
	return closeBlobErr
}

func clipLoudnormTarget(specFilters []ffmpeg.FilterSpec, stack json.RawMessage) (float64, bool) {
	specs := specFilters
	if len(specs) == 0 && len(stack) > 0 && string(stack) != "[]" && string(stack) != "null" {
		_ = json.Unmarshal(stack, &specs)
	}
	for _, s := range specs {
		if s.Type != "normalize" {
			continue
		}
		mode, _ := s.Params["mode"].(string)
		if mode == "rms" || mode == "peak" {
			return 0, false
		}
		target := ffmpeg.DefaultLoudnessI
		if s.Params != nil {
			switch v := s.Params["target"].(type) {
			case float64:
				target = v
			case string:
				if f, err := strconv.ParseFloat(v, 64); err == nil {
					target = f
				}
			}
		}
		return target, true
	}
	return 0, false
}

func findVideoFile(dir, videoID string) string {
	for _, ext := range videoExtensions {
		p := filepath.Join(dir, videoID+".video"+ext)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func uuidString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmt.Sprintf(
		"%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		b[0], b[1], b[2], b[3],
		b[4], b[5],
		b[6], b[7],
		b[8], b[9],
		b[10], b[11], b[12], b[13], b[14], b[15],
	)
}

func cleanupMissingExportFiles(ctx context.Context, dbc *db.DatabaseConnection) {
	q := dbc.Queries(ctx)

	exports, err := q.FindReadyExportsWithMissingFiles(ctx)
	if err != nil {
		slog.Error("failed to find ready exports for cleanup", "error", err)
		return
	}

	requeuedCount := 0
	for _, exp := range exports {
		if _, err := os.Stat(exp.FilePath); err != nil {
			if reqErr := q.RequeueClipExport(ctx, exp.ID); reqErr != nil {
				slog.Error("failed to requeue missing export", "export_id", uuidString(exp.ID), "error", reqErr)
				continue
			}
			requeuedCount++
			slog.Info("requeued export with missing file", "export_id", uuidString(exp.ID), "clip_id", uuidString(exp.ClipID), "file_path", exp.FilePath)
		}
	}

	if requeuedCount > 0 {
		slog.Info("startup cleanup complete", "requeued_exports", requeuedCount)
	}
}
