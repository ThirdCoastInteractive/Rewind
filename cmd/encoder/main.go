package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"thirdcoast.systems/rewind/internal/application"
	"thirdcoast.systems/rewind/internal/config"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/ffmpeg"
	"thirdcoast.systems/rewind/pkg/utils/crops"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Info("Starting encoder service")

	conf, err := config.LoadConfig(ctx)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	if conf.DatabaseRetries <= 0 {
		conf.DatabaseRetries = 10
	}

	exportsDir := strings.TrimSpace(os.Getenv("EXPORTS_DIR"))
	if exportsDir == "" {
		exportsDir = "/exports"
	}
	if err := os.MkdirAll(filepath.Join(exportsDir, "clips"), 0o755); err != nil {
		slog.Error("failed to create exports dir", "dir", exportsDir, "error", err)
		os.Exit(1)
	}

	downloadsDir := strings.TrimSpace(os.Getenv("DOWNLOADS_DIR"))
	if downloadsDir == "" {
		downloadsDir = "/downloads"
	}

	pool, err := application.OpenDBPoolWithRetry(ctx, *conf)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	dbc, err := db.NewDatabaseConnection(ctx, pool)
	if err != nil {
		slog.Error("failed to create database connection", "error", err)
		os.Exit(1)
	}
	defer dbc.Close()

	workers := envInt("ENCODER_WORKERS", 2)
	// Use hostname (container ID) for unique worker ID since PID is always 1 in containers
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = fmt.Sprintf("pid-%d", os.Getpid())
	}
	workerID := fmt.Sprintf("encoder-%s", hostname)

	// NOTE: We intentionally do NOT run PID-based orphan cleanup on startup anymore.
	// Each container has a unique hostname, so we can't clean up other containers' work.
	// But we DO reset status for stuck "processing" exports so they get re-queued.
	slog.Info("Recovering stuck exports from previous service instances")
	if err := dbc.Queries(ctx).ResetStuckExports(ctx); err != nil {
		slog.Error("failed to recover stuck exports", "error", err)
	}

	// Cleanup: requeue any "ready" exports where the file is missing
	cleanupMissingExportFiles(ctx, dbc)

	wake := make(chan struct{}, 1)
	go listenAndSignal(ctx, conf.DatabaseDSN, "clip_exports", wake)

	slog.Info("Encoder workers started", "workers", workers, "worker_id", workerID)
	for i := 0; i < workers; i++ {
		go encoderWorker(ctx, dbc, exportsDir, downloadsDir, workerID, wake)
	}

	<-ctx.Done()
	slog.Info("Encoder service stopping")
}

func encoderWorker(ctx context.Context, dbc *db.DatabaseConnection, exportsDir, downloadsDir, workerID string, wake <-chan struct{}) {
	q := dbc.Queries(ctx)
	for {
		if ctx.Err() != nil {
			return
		}

		// Process jobs until queue is empty
		for {
			exportRow, err := q.FindAndLockPendingClipExport(ctx, &workerID)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					break
				}
				slog.Error("failed to find/lock pending export", "error", err)
				time.Sleep(2 * time.Second)
				break
			}

			if err := processExport(ctx, q, exportsDir, downloadsDir, exportRow); err != nil {
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
			// new job notification
		case <-time.After(5 * time.Second):
			// periodic poll
		}
	}
}

func processExport(ctx context.Context, q *db.Queries, exportsDir, downloadsDir string, exportRow *db.FindAndLockPendingClipExportRow) error {
	exportID := uuidString(exportRow.ID)
	clipID := uuidString(exportRow.ClipID)

	slog.Info("processing export", "export_id", exportID, "clip_id", clipID, "variant", exportRow.Variant)

	// Get clip data
	clipData, err := q.GetClipForExport(ctx, exportRow.ClipID)
	if err != nil {
		return fmt.Errorf("failed to get clip data: %w", err)
	}

	// Find video file
	videoID := uuidString(clipData.VideoID)
	videoDir := filepath.Join(downloadsDir, videoID)
	inputPath := findVideoFile(videoDir, videoID)
	if inputPath == "" {
		return fmt.Errorf("video file not found in %s", videoDir)
	}

	// Create output directory
	clipExportDir := filepath.Join(exportsDir, "clips", clipID)
	if err := os.MkdirAll(clipExportDir, 0o755); err != nil {
		return fmt.Errorf("failed to create export dir: %w", err)
	}

	// Determine codec presets and file extension based on format
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

	// Update file path in DB
	if err := q.UpdateClipExportFilePath(ctx, &db.UpdateClipExportFilePathParams{
		ID:       exportRow.ID,
		FilePath: outputPath,
	}); err != nil {
		slog.Warn("failed to update export file path", "error", err)
	}

	start := time.Duration(clipData.StartTs * float64(time.Second))
	end := time.Duration((clipData.StartTs + clipData.Duration) * float64(time.Second))

	// Build options using format-aware codec presets
	opts := ffmpeg.Flatten(videoPreset)
	if audioPreset != nil {
		opts = append(opts, ffmpeg.Flatten(audioPreset)...)
	}
	opts = append(opts,
		ffmpeg.Metadata("encoded_by", "Rewind Video Archive"),
		ffmpeg.Metadata("comment", "Exported with Rewind https://github.com/ThirdCoastInteractive/Rewind"),
	)

	// Embed clip title as metadata
	if clipData.ClipTitle != "" {
		opts = append(opts, ffmpeg.Metadata("title", clipData.ClipTitle))
	}

	// Embed filter stack as metadata so exports are self-documenting
	if len(clipData.FilterStack) > 0 && string(clipData.FilterStack) != "[]" && string(clipData.FilterStack) != "null" {
		opts = append(opts, ffmpeg.Metadata("rewind_filter_stack", string(clipData.FilterStack)))
	}

	// Embed crop info as metadata when a crop variant is used
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

	// Apply filters: spec-based pipeline takes precedence, otherwise fall back to legacy crop variant
	var specApplied bool
	if len(exportRow.Spec) > 0 {
		var spec ffmpeg.ExportSpec
		if err := json.Unmarshal(exportRow.Spec, &spec); err != nil {
			slog.Warn("failed to parse export spec, falling back to variant", "error", err)
		} else if len(spec.Filters) > 0 {
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
		// Legacy crop variant handling
		variant := exportRow.Variant
		if strings.HasPrefix(variant, "crop:") {
			cropID := strings.TrimPrefix(variant, "crop:")
			if filter := crops.BuildCropFilterByID(clipData.Crops, cropID); filter != "" {
				opts = append(opts, ffmpeg.Filter(filter))
				// Cropping can produce odd dimensions which h264 rejects
				opts = append(opts, ffmpeg.EvenDimensions())
			}
		}
	}

	// Progress channel
	progressChan := make(chan ffmpeg.Progress, 100)

	// Build command with seek + duration
	allOpts := append([]ffmpeg.Option{ffmpeg.SeekTo(start, end)}, opts...)
	cmd := ffmpeg.NewCommand(inputPath, outputPath, allOpts...)

	// Start with progress tracking
	proc, err := cmd.StartWithProgress(ctx, progressChan)
	if err != nil {
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	// Store PID for potential cleanup if we crash
	pid := int32(proc.PID())
	if err := q.UpdateClipExportPID(ctx, &db.UpdateClipExportPIDParams{
		ID:  exportRow.ID,
		Pid: &pid,
	}); err != nil {
		slog.Warn("failed to store ffmpeg PID", "error", err, "pid", pid)
	}

	// Process progress updates
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

	// Wait for completion
	if err := proc.Wait(); err != nil {
		_ = os.Remove(outputPath)
		return fmt.Errorf("ffmpeg failed: %w", err)
	}

	// Verify output exists
	st, err := os.Stat(outputPath)
	if err != nil {
		return fmt.Errorf("output file missing: %w", err)
	}

	// Validate output is a playable media file
	probe, probeErr := ffmpeg.Probe(ctx, outputPath)
	if probeErr != nil {
		_ = os.Remove(outputPath)
		return fmt.Errorf("output validation failed (ffprobe): %w", probeErr)
	}
	if probe.Duration < 0.5 {
		_ = os.Remove(outputPath)
		return fmt.Errorf("output validation failed: duration too short (%.2fs)", probe.Duration)
	}

	// Mark ready
	if err := q.FinishClipExportReady(ctx, &db.FinishClipExportReadyParams{
		ID:        exportRow.ID,
		FilePath:  outputPath,
		SizeBytes: st.Size(),
	}); err != nil {
		return fmt.Errorf("failed to mark export ready: %w", err)
	}

	slog.Info("export complete", "export_id", exportID, "clip_id", clipID, "size_bytes", st.Size())
	return nil
}

var videoExtensions = []string{".webm", ".mp4", ".mkv", ".mov", ".avi"}

func findVideoFile(dir, videoID string) string {
	for _, ext := range videoExtensions {
		p := filepath.Join(dir, videoID+".video"+ext)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func listenAndSignal(ctx context.Context, dsn, channel string, signalCh chan<- struct{}) {
	for {
		if ctx.Err() != nil {
			return
		}

		conn, err := pgxpool.New(ctx, dsn)
		if err != nil {
			slog.Error("failed to connect for LISTEN", "channel", channel, "error", err)
			time.Sleep(5 * time.Second)
			continue
		}

		c, err := conn.Acquire(ctx)
		if err != nil {
			slog.Error("failed to acquire connection for LISTEN", "channel", channel, "error", err)
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		_, err = c.Exec(ctx, "LISTEN "+channel)
		if err != nil {
			slog.Error("failed to LISTEN", "channel", channel, "error", err)
			c.Release()
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		slog.Info("Listening for notifications", "channel", channel)

		for {
			if ctx.Err() != nil {
				c.Release()
				conn.Close()
				return
			}

			_, err := c.Conn().WaitForNotification(ctx)
			if err != nil {
				slog.Error("wait for notification failed", "channel", channel, "error", err)
				c.Release()
				conn.Close()
				break
			}

			select {
			case signalCh <- struct{}{}:
			default:
			}
		}
	}
}

func envInt(name string, def int) int {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
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

// cleanupMissingExportFiles finds "ready" exports where the file is missing and requeues them
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
			// File is missing - requeue
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
