package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	_ "image/jpeg"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"thirdcoast.systems/rewind/internal/application"
	"thirdcoast.systems/rewind/internal/config"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/videoid"
	"thirdcoast.systems/rewind/pkg/ffmpeg"
	"thirdcoast.systems/rewind/pkg/videoinfo"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Info("Starting ingest service")

	conf, err := config.LoadConfig(ctx)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	logWhisperStartupInfo()
	if conf.DatabaseRetries <= 0 {
		conf.DatabaseRetries = 10
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

	// Recover orphaned jobs stuck in "processing" from previous crashes/restarts
	slog.Info("Recovering stuck ingest jobs from previous service instances")
	if err := dbc.Queries(ctx).RecoverStuckIngestJobs(ctx); err != nil {
		slog.Error("failed to recover stuck ingest jobs", "error", err)
		// Non-fatal - continue startup
	}

	// Fail jobs that have exceeded max retry attempts so they stop wasting workers
	if n, err := dbc.Queries(ctx).FailExcessiveRetryIngestJobs(ctx); err != nil {
		slog.Error("failed to fail excessive retry ingest jobs", "error", err)
	} else if n > 0 {
		slog.Warn("permanently failed ingest jobs exceeding max retries", "count", n)
	}

	// Periodically recover stuck jobs and fail excessive retries (not just on startup).
	// This prevents long-running ffmpeg operations from permanently blocking the queue.
	go func() {
		ticker := time.NewTicker(2 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := dbc.Queries(ctx).RecoverStuckIngestJobs(ctx); err != nil {
					slog.Error("periodic: failed to recover stuck ingest jobs", "error", err)
				}
				if n, err := dbc.Queries(ctx).FailExcessiveRetryIngestJobs(ctx); err != nil {
					slog.Error("periodic: failed to fail excessive retry ingest jobs", "error", err)
				} else if n > 0 {
					slog.Warn("periodic: permanently failed ingest jobs exceeding max retries", "count", n)
				}
			}
		}
	}()

	// Best-effort: catch up missing thumbnails/previews/assets for already-ingested videos.
	// Each ingest worker performs a small catchup unit on startup so scaled ingest replicas
	// actually contribute to asset backfill.

	workers := envInt("INGEST_WORKERS", 2)
	wake := make(chan struct{}, 1)
	go listenAndSignal(ctx, conf.DatabaseDSN, "ingest_jobs", wake)

	slog.Info("Ingest workers started", "workers", workers)
	for i := 0; i < workers; i++ {
		go ingestWorker(ctx, dbc, wake)
	}

	<-ctx.Done()
	slog.Info("Ingest service stopping")
}

// runProbeBackfill fills in probe_data for videos that don't have it yet.
func runProbeBackfill(ctx context.Context, dbc *db.DatabaseConnection) {
	q := dbc.Queries(ctx)
	rows, err := q.ListVideosNeedingProbe(ctx, 50)
	if err != nil {
		slog.Warn("probe backfill query failed", "error", err)
		return
	}
	if len(rows) == 0 {
		return
	}
	slog.Info("probe backfill start", "videos_needing_probe", len(rows))
	for _, row := range rows {
		if row.VideoPath == nil {
			continue
		}
		videoPath := strings.TrimSpace(*row.VideoPath)
		if videoPath == "" {
			continue
		}
		probeResult, probeErr := ffmpeg.Probe(ctx, videoPath)
		if probeErr != nil {
			slog.Warn("probe backfill failed", "video_id", row.ID, "error", probeErr)
			continue
		}
		pj, marshalErr := json.Marshal(probeResult.RawJSON)
		if marshalErr != nil {
			continue
		}
		if err := q.UpdateVideoProbeData(ctx, &db.UpdateVideoProbeDataParams{ID: row.ID, ProbeData: videoinfo.NewProbeInfo(pj)}); err != nil {
			slog.Warn("probe backfill update failed", "video_id", row.ID, "error", err)
		} else {
			slog.Info("probe backfill stored", "video_id", row.ID,
				"video_streams", probeResult.VideoStreams,
				"audio_streams", probeResult.AudioStreams)
		}
	}
}

// runAssetCatchupUnit performs a small amount of asset backfill work (best-effort).
// It queries for videos that are actually missing assets (incomplete assets_status)
// and processes a small batch. Successfully processed videos update their assets_status
// and drop out of future queries. Errors are collected per-asset and stored in
// assets_status so they can be surfaced to admins.
func runAssetCatchupUnit(ctx context.Context, dbc *db.DatabaseConnection) {
	// Give the service a moment to start up and connect.
	select {
	case <-ctx.Done():
		return
	case <-time.After(250 * time.Millisecond):
	}

	const maxVideos = 8
	processed := 0
	q := dbc.Queries(ctx)

	rows, err := q.ListVideosForAssetCatchup(ctx, int32(maxVideos))
	if err != nil {
		slog.Warn("asset catchup unit query failed", "error", err)
		return
	}
	if len(rows) == 0 {
		return
	}
	slog.Info("asset catchup unit start", "videos_needing_assets", len(rows))

	for _, row := range rows {
		processed++

		videoID := row.ID
		if row.VideoPath == nil {
			continue
		}
		videoPath := strings.TrimSpace(*row.VideoPath)
		thumbPath := row.ThumbnailPath
		fileHash := row.FileHash
		durationSeconds := row.DurationSeconds

		if videoPath == "" {
			continue
		}

		slog.Info("asset catchup scan", "video_id", videoID, "video_path", videoPath, "thumb_path", derefString(thumbPath), "has_hash", fileHash != nil && strings.TrimSpace(*fileHash) != "", "duration_seconds", durationSeconds)

		lockID := advisoryLockID("asset-catchup", videoID)
		conn, err := dbc.Acquire(ctx)
		if err != nil {
			slog.Warn("asset catchup lock acquire failed", "video_id", videoID, "error", err)
			continue
		}
		q := db.New(conn)
		acquired, err := q.TryAdvisoryLock(ctx, lockID)
		if err != nil || !acquired {
			if err != nil {
				slog.Warn("asset catchup lock error", "video_id", videoID, "error", err)
			} else {
				slog.Info("asset catchup lock busy", "video_id", videoID)
			}
			conn.Release()
			continue
		}

		// Migration: move from old DB paths into canonical /downloads/<uuid>/ and rename into uuid.<kind>.*.
		migratedVideoPath, migratedThumbPath, _ := migrateVideoAssetsToCanonicalDir(videoID, videoPath, thumbPath)
		if strings.TrimSpace(migratedVideoPath) != "" && strings.TrimSpace(migratedVideoPath) != videoPath {
			videoPath = strings.TrimSpace(migratedVideoPath)
			slog.Info("asset catchup migrated video path", "video_id", videoID, "new_path", videoPath)
			var idUUID pgtype.UUID
			if err := idUUID.Scan(videoID); err == nil {
				_ = q.UpdateVideoPath(ctx, &db.UpdateVideoPathParams{ID: idUUID, VideoPath: &videoPath})
			}
		}
		if migratedThumbPath != nil && strings.TrimSpace(*migratedThumbPath) != "" {
			slog.Info("asset catchup migrated thumb path", "video_id", videoID, "new_path", strings.TrimSpace(*migratedThumbPath))
			var idUUID pgtype.UUID
			if err := idUUID.Scan(videoID); err == nil {
				_ = q.UpdateVideoThumbnailPath(ctx, &db.UpdateVideoThumbnailPathParams{ID: idUUID, ThumbnailPath: migratedThumbPath})
			}
		}

		logDirContents("asset catchup dir", filepath.Dir(videoPath))

		var idUUID pgtype.UUID
		_ = idUUID.Scan(videoID)

		// Collect errors from each asset generation step.
		assetErrors := map[string]string{}

		// Probe video file first - if ffprobe can't read it, all asset generation will fail.
		// Also store probe data if not already present.
		probeResult, probeErr := ffmpeg.Probe(ctx, videoPath)
		if probeErr != nil {
			slog.Warn("asset catchup video unreadable", "video_id", videoID, "error", probeErr)
			assetErrors["video_file"] = probeErr.Error()
		} else {
			// Backfill probe_data if missing
			if pj, marshalErr := json.Marshal(probeResult.RawJSON); marshalErr == nil {
				_ = q.UpdateVideoProbeData(ctx, &db.UpdateVideoProbeDataParams{ID: idUUID, ProbeData: videoinfo.NewProbeInfo(pj)})
			}
		}

		// File hash: compute if missing
		if fileHash == nil || strings.TrimSpace(*fileHash) == "" {
			if h, s, err := computeFileHashAndSize(videoPath); err == nil {
				slog.Info("asset catchup computed file hash", "video_id", videoID, "file_hash", h, "file_size", s)
				_ = q.UpdateVideoFileHashAndSize(ctx, &db.UpdateVideoFileHashAndSizeParams{ID: idUUID, FileHash: &h, FileSize: &s})
				fileHash = &h
			} else {
				slog.Warn("asset catchup hash failed", "video_id", videoID, "error", err)
				assetErrors["file_hash"] = err.Error()
			}
		}

		// Only attempt asset generation if the video file is readable
		if _, hasProbeErr := assetErrors["video_file"]; !hasProbeErr {
			// Thumbnail: find existing or generate
			if p, err := generateVideoThumbnail(ctx, videoPath, videoID, false); err == nil {
				_ = q.UpdateVideoThumbnailPath(ctx, &db.UpdateVideoThumbnailPathParams{ID: idUUID, ThumbnailPath: p})
			} else {
				slog.Warn("asset catchup thumbnail failed", "video_id", videoID, "error", err)
				assetErrors["thumbnail"] = err.Error()
			}

			// Preview
			if err := generateVideoPreview(ctx, videoPath, videoID, false); err != nil {
				slog.Warn("asset catchup preview failed", "video_id", videoID, "error", err)
				assetErrors["preview"] = err.Error()
			}

			// Seek sprites
			if _, err := generateVideoSeekAssets(ctx, videoPath, videoID, durationSeconds, false); err != nil {
				slog.Warn("asset catchup seek assets failed", "video_id", videoID, "error", err)
				assetErrors["seek"] = err.Error()
			}

			// Waveform
			if _, err := generateVideoWaveform(ctx, videoPath, videoID, durationSeconds, false); err != nil {
				slog.Warn("asset catchup waveform failed", "video_id", videoID, "error", err)
				assetErrors["waveform"] = err.Error()
			}

			// Captions: find existing or generate via Whisper
			if _, _, ok := findCanonicalCaptionFilePath(filepath.Dir(videoPath), videoID); !ok && whisperEnabled() {
				if p, l, wErr := generateCaptionsWithWhisper(ctx, videoPath, videoID, filepath.Dir(videoPath)); wErr != nil {
					slog.Warn("asset catchup whisper failed", "video_id", videoID, "error", wErr)
					assetErrors["captions"] = wErr.Error()
				} else if iErr := ingestTranscriptFile(ctx, q, idUUID, l, p); iErr != nil {
					slog.Warn("asset catchup whisper transcript ingest failed", "video_id", videoID, "error", iErr)
					assetErrors["captions"] = iErr.Error()
				} else {
					slog.Info("asset catchup generated captions via whisper", "video_id", videoID, "lang", l)
				}
			}
		}

		// Build final status: disk verification + error tracking
		status := verifyAllAssetStatus(videoPath, videoID, fileHash)

		if len(assetErrors) > 0 {
			// Increment error count, store errors and timestamp
			prevCount := 0
			if row.AssetsStatus != nil {
				if v, ok := row.AssetsStatus["_error_count"]; ok {
					if f, ok := v.(float64); ok {
						prevCount = int(f)
					}
				}
			}
			status["_error_count"] = prevCount + 1
			status["_last_error_at"] = time.Now().UTC().Format(time.RFC3339)
			status["_errors"] = assetErrors
			slog.Warn("asset catchup completed with errors",
				"video_id", videoID, "error_count", prevCount+1, "errors", assetErrors)
		} else {
			// All assets OK - clear any previous error tracking
			status["_error_count"] = 0
			status["_errors"] = map[string]string{}
		}

		if err := updateVideoAssetsStatus(ctx, q, videoID, status); err != nil {
			slog.Warn("asset catchup assets_status update failed", "video_id", videoID, "error", err)
		}

		_, _ = q.AdvisoryUnlock(ctx, lockID)
		conn.Release()

		// Small throttle to keep CPU/disk sane.
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Millisecond):
		}
	}

	if processed > 0 {
		slog.Info("asset catchup unit complete", "videos_scanned", processed)
	}

}

// verifyWaveformAssets checks if waveform assets exist and are valid.
func verifyWaveformAssets(videoPath string) bool {
	wfDir := filepath.Join(filepath.Dir(videoPath), "waveform")

	// Check for no-audio marker (videos without audio are valid)
	if _, err := os.Stat(filepath.Join(wfDir, ".no-audio")); err == nil {
		return true
	}

	// Check for manifest and peaks
	manifestPath := filepath.Join(wfDir, "waveform.json")
	peaksPath := filepath.Join(wfDir, "peaks.i16")

	if _, err := os.Stat(manifestPath); err != nil {
		return false
	}
	if _, err := os.Stat(peaksPath); err != nil {
		return false
	}

	// Validate manifest content
	b, err := os.ReadFile(manifestPath)
	if err != nil {
		return false
	}

	var m waveformManifest
	if err := json.Unmarshal(b, &m); err != nil {
		return false
	}

	if m.Format != waveformFormatV1 || m.BucketMS != 100 || m.SampleRateHz != 8000 {
		return false
	}

	return true
}

// generateVideoThumbnail generates a thumbnail for a video, optionally deleting the existing one first.
func generateVideoThumbnail(ctx context.Context, videoPath, videoID string, forceRegenerate bool) (*string, error) {
	videoDir := filepath.Dir(videoPath)
	thumbPath := filepath.Join(videoDir, videoID+".thumbnail.jpg")

	if forceRegenerate {
		if err := os.Remove(thumbPath); err != nil && !os.IsNotExist(err) {
			slog.Warn("failed to delete existing thumbnail", "path", thumbPath, "error", err)
		} else if err == nil {
			slog.Info("deleted existing thumbnail for regeneration", "path", thumbPath)
		}
		for _, variant := range thumbnailVariants {
			variantPath := thumbnailVariantPath(videoDir, videoID, variant.Label)
			if err := os.Remove(variantPath); err != nil && !os.IsNotExist(err) {
				slog.Warn("failed to delete existing thumbnail variant", "path", variantPath, "error", err)
			} else if err == nil {
				slog.Info("deleted existing thumbnail variant for regeneration", "path", variantPath)
			}
		}
	}

	p, err := generateThumbnail(ctx, videoPath)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// generateVideoPreview generates a preview MP4 for a video, optionally deleting the existing one first.
func generateVideoPreview(ctx context.Context, videoPath, videoID string, forceRegenerate bool) error {
	videoDir := filepath.Dir(videoPath)
	previewPath := filepath.Join(videoDir, videoID+".preview.mp4")

	if forceRegenerate {
		if err := os.Remove(previewPath); err != nil && !os.IsNotExist(err) {
			slog.Warn("failed to delete existing preview", "path", previewPath, "error", err)
		} else if err == nil {
			slog.Info("deleted existing preview for regeneration", "path", previewPath)
		}
	}

	return generatePreviewMP4(ctx, videoPath)
}

// generateVideoSeekAssets generates seek sprite sheets for a video, optionally deleting existing ones first.
func generateVideoSeekAssets(ctx context.Context, videoPath, videoID string, durationSeconds *int32, forceRegenerate bool) (bool, error) {
	if forceRegenerate {
		videoDir := filepath.Dir(videoPath)
		seekDir := filepath.Join(videoDir, "seek")
		if err := os.RemoveAll(seekDir); err != nil && !os.IsNotExist(err) {
			slog.Warn("failed to delete existing seek directory", "path", seekDir, "error", err)
		} else if err == nil {
			slog.Info("deleted existing seek directory for regeneration", "path", seekDir)
		}
	}

	return ensureSeekAssets(ctx, videoPath, durationSeconds)
}

// generateVideoWaveform generates waveform data for a video, optionally deleting existing data first.
func generateVideoWaveform(ctx context.Context, videoPath, videoID string, durationSeconds *int32, forceRegenerate bool) (bool, error) {
	if forceRegenerate {
		videoDir := filepath.Dir(videoPath)
		waveformDir := filepath.Join(videoDir, "waveform")
		if err := os.RemoveAll(waveformDir); err != nil && !os.IsNotExist(err) {
			slog.Warn("failed to delete existing waveform directory", "path", waveformDir, "error", err)
		} else if err == nil {
			slog.Info("deleted existing waveform directory for regeneration", "path", waveformDir)
		}
	}

	return ensureWaveformAssets(ctx, videoPath, durationSeconds)
}

func advisoryLockID(scope, id string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(scope))
	_, _ = h.Write([]byte(":"))
	_, _ = h.Write([]byte(id))
	return int64(h.Sum64())
}

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

func logDirContents(label string, dir string) {
	if strings.TrimSpace(dir) == "" {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		slog.Warn(label+" read failed", "dir", dir, "error", err)
		return
	}
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name = name + "/"
		}
		files = append(files, name)
	}
	slog.Info(label+" contents", "dir", dir, "entries", files)
}

// verifyAllAssetStatus checks which generated assets exist on disk for a video.
func verifyAllAssetStatus(videoPath, videoID string, fileHash *string) map[string]any {
	status := map[string]any{}
	dir := filepath.Dir(videoPath)

	// Video file
	_, err := os.Stat(videoPath)
	status["video_file"] = err == nil

	// File hash
	status["file_hash"] = fileHash != nil && strings.TrimSpace(*fileHash) != ""

	// Thumbnail
	_, err = os.Stat(filepath.Join(dir, videoID+".thumbnail.jpg"))
	status["thumbnail"] = err == nil

	// Preview
	_, err = os.Stat(filepath.Join(dir, videoID+".preview.mp4"))
	status["preview"] = err == nil

	// Seek sprites
	if levelStatus, sErr := verifySeekAssetsDetailed(videoPath); sErr == nil {
		status["seek"] = levelStatus
	} else {
		status["seek"] = false
	}

	// Waveform
	status["waveform"] = verifyWaveformAssets(videoPath)

	// Captions
	_, _, capOK := findCanonicalCaptionFilePath(dir, videoID)
	status["captions"] = capOK

	// HLS
	status["hls"] = hasHLS(videoPath)

	return status
}

func updateVideoAssetsStatus(ctx context.Context, q db.Querier, videoID string, status map[string]any) error {
	if strings.TrimSpace(videoID) == "" || len(status) == 0 {
		return nil
	}
	var videoUUID pgtype.UUID
	if err := videoUUID.Scan(videoID); err != nil {
		return err
	}
	return q.UpdateVideoAssetsStatus(ctx, &db.UpdateVideoAssetsStatusParams{
		ID:           videoUUID,
		AssetsStatus: db.AssetMap(status),
	})
}

func ingestWorker(ctx context.Context, dbc *db.DatabaseConnection, wake <-chan struct{}) {
	q := dbc.Queries(ctx)

	// Ensure each worker contributes at least a little to asset catchup.
	// This is intentionally best-effort and bounded.
	runAssetCatchupUnit(ctx, dbc)

	// Backfill probe_data for existing videos that don't have it yet.
	runProbeBackfill(ctx, dbc)

	catchupTicker := time.NewTicker(2 * time.Minute)
	defer catchupTicker.Stop()

	for {
		if ctx.Err() != nil {
			return
		}

		for {
			job, err := q.DequeueIngestJob(ctx)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					break
				}
				slog.Error("failed to dequeue ingest job", "error", err)
				time.Sleep(2 * time.Second)
				break
			}

			// Dispatch to the appropriate handler based on job type
			// Regeneration jobs have no info_json_path or spool_dir
			isRegenerationJob := (job.InfoJsonPath == nil || strings.TrimSpace(*job.InfoJsonPath) == "") &&
				(job.SpoolDir == nil || strings.TrimSpace(*job.SpoolDir) == "")

			if isRegenerationJob {
				if err := processAssetRegenerationJob(ctx, q, job); err != nil {
					slog.Error("asset regeneration job failed", "ingest_job_id", job.IngestJobID, "error", err)
					errMsg := err.Error()
					_ = q.MarkIngestJobFailed(ctx, &db.MarkIngestJobFailedParams{ID: job.IngestJobID, LastError: &errMsg})
				}
			} else {
				if err := processIngestJob(ctx, q, job); err != nil {
					slog.Error("ingest job failed", "ingest_job_id", job.IngestJobID, "error", err)
					errMsg := err.Error()
					_ = q.MarkIngestJobFailed(ctx, &db.MarkIngestJobFailedParams{ID: job.IngestJobID, LastError: &errMsg})
				}
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-wake:
		case <-catchupTicker.C:
			runAssetCatchupUnit(ctx, dbc)
		case <-time.After(5 * time.Second):
		}
	}
}

// processAssetRegenerationJob handles regeneration of assets for an existing video
func processAssetRegenerationJob(ctx context.Context, q *db.Queries, job *db.DequeueIngestJobRow) error {
	slog.Info("processing asset regeneration job", "ingest_job_id", job.IngestJobID, "download_job_id", job.DownloadJobID, "video_id", job.VideoID)

	// VideoID is now returned directly from DequeueIngestJob
	if !job.VideoID.Valid {
		return errors.New("asset regeneration job has no video_id")
	}

	// Get the existing video by ID
	videoRow, err := q.GetVideoByID(ctx, job.VideoID)
	if err != nil {
		return fmt.Errorf("get video by id: %w", err)
	}

	videoID := videoRow.ID.String()

	// Resolve video_path: use DB value, or discover on disk if NULL
	var videoPath string
	if videoRow.VideoPath != nil && strings.TrimSpace(*videoRow.VideoPath) != "" {
		videoPath = *videoRow.VideoPath
	} else {
		// video_path not set in DB - try to find the file at the canonical location
		dir := filepath.Join("/downloads", videoID)
		files, _ := filepath.Glob(filepath.Join(dir, "*"))
		discovered := pickPreferredVideoPath(files)
		if discovered == "" {
			return fmt.Errorf("video %s has no video_path and no video file found in %s", videoID, dir)
		}
		videoPath = discovered
		slog.Info("discovered video file on disk (video_path was NULL)", "video_id", videoID, "path", videoPath)

		// Persist the discovered path so future operations don't need to re-discover
		if err := q.UpdateVideoPath(ctx, &db.UpdateVideoPathParams{ID: videoRow.ID, VideoPath: &videoPath}); err != nil {
			slog.Warn("failed to persist discovered video_path", "video_id", videoID, "error", err)
		}
	}

	// Parse the existing info JSON to get duration
	var info ytdlpInfo
	_ = json.Unmarshal(videoRow.Info.RawJSON(), &info)
	norm := normalizeInfo(videoRow.Info.RawJSON())

	slog.Info("regenerating assets", "video_id", videoID, "video_path", videoPath, "duration", norm.DurationSeconds)

	// Determine which assets to regenerate
	scope := "all"
	if job.AssetScope != nil && strings.TrimSpace(*job.AssetScope) != "" {
		scope = strings.TrimSpace(*job.AssetScope)
	}
	slog.Info("asset regeneration scope", "video_id", videoID, "scope", scope)

	// Regenerate thumbnail
	if scope == "all" || scope == "thumbnail" {
		if p, genErr := generateVideoThumbnail(ctx, videoPath, videoID, true); genErr != nil {
			slog.Warn("failed to generate thumbnail", "video_id", videoID, "error", genErr)
		} else {
			slog.Info("regenerated thumbnail", "video_id", videoID, "path", *p)
			if err := q.UpdateVideoThumbnailPath(ctx, &db.UpdateVideoThumbnailPathParams{ID: videoRow.ID, ThumbnailPath: p}); err != nil {
				slog.Warn("failed to update thumbnail path", "video_id", videoID, "error", err)
			}
		}
	}

	// Regenerate preview
	if scope == "all" || scope == "preview" {
		if genErr := generateVideoPreview(ctx, videoPath, videoID, true); genErr != nil {
			slog.Warn("failed to generate preview", "video_id", videoID, "error", genErr)
		} else {
			slog.Info("regenerated preview", "video_id", videoID)
		}
	}

	// Regenerate seek sprites
	if scope == "all" || scope == "seek" {
		if ok, genErr := generateVideoSeekAssets(ctx, videoPath, videoID, norm.DurationSeconds, true); genErr != nil {
			slog.Warn("failed to generate seek assets", "video_id", videoID, "error", genErr)
		} else if ok {
			slog.Info("regenerated seek assets", "video_id", videoID)
		}
	}

	// Regenerate waveform
	if scope == "all" || scope == "waveform" {
		if ok, genErr := generateVideoWaveform(ctx, videoPath, videoID, norm.DurationSeconds, true); genErr != nil {
			slog.Warn("failed to generate waveform assets", "video_id", videoID, "error", genErr)
		} else if ok {
			slog.Info("regenerated waveform", "video_id", videoID)
		}
	}

	// Regenerate captions via Whisper
	if scope == "all" || scope == "captions" {
		dir := filepath.Dir(videoPath)
		if whisperEnabled() {
			if p, l, err := generateCaptionsWithWhisper(ctx, videoPath, videoID, dir); err != nil {
				slog.Warn("whisper caption regeneration failed", "video_id", videoID, "error", err)
			} else {
				if err := ingestTranscriptFile(ctx, q, videoRow.ID, l, p); err != nil {
					slog.Warn("failed to ingest regenerated whisper transcript", "video_id", videoID, "path", p, "error", err)
				} else {
					slog.Info("regenerated captions via whisper", "video_id", videoID, "lang", l)
				}
			}
		} else {
			slog.Info("skipping caption regeneration: whisper not enabled", "video_id", videoID)
		}
	}

	// Regenerate HLS (demux + segment)
	if scope == "all" || scope == "hls" {
		if _, hlsErr := regenerateHLS(ctx, videoPath, videoID); hlsErr != nil {
			slog.Warn("HLS regeneration failed", "video_id", videoID, "error", hlsErr)
		} else {
			slog.Info("regenerated HLS", "video_id", videoID)
		}
		// Ensure streams manifest is up-to-date
		writeStreamsManifest(ctx, videoPath)
	}

	slog.Info("asset regeneration complete", "video_id", videoID)

	if err := updateVideoAssetsStatus(ctx, q, videoID, verifyAllAssetStatus(videoPath, videoID, videoRow.FileHash)); err != nil {
		slog.Warn("failed to update assets_status after regeneration", "video_id", videoID, "error", err)
	}

	// Link the download job to the video
	if err := q.LinkDownloadJobVideo(ctx, &db.LinkDownloadJobVideoParams{ID: job.DownloadJobID, VideoID: videoRow.ID}); err != nil {
		slog.Warn("failed to link download job video", "error", err)
	}

	return q.MarkIngestJobSucceeded(ctx, job.IngestJobID)
}

func processIngestJob(ctx context.Context, q *db.Queries, job *db.DequeueIngestJobRow) error {
	// This handles normal ingest from a download job with info.json
	if job.InfoJsonPath == nil || strings.TrimSpace(*job.InfoJsonPath) == "" {
		return errors.New("missing info_json_path on download job")
	}
	infoPath := *job.InfoJsonPath

	b, err := os.ReadFile(infoPath)
	if err != nil {
		// Recovery: If spool is gone but video_id exists, convert to asset regeneration job
		if job.VideoID.Valid && os.IsNotExist(err) {
			slog.Warn("spool cleaned up but video exists - converting to asset regeneration",
				"ingest_job_id", job.IngestJobID, "video_id", job.VideoID)
			return processAssetRegenerationJob(ctx, q, job)
		}
		return fmt.Errorf("read info json: %w", err)
	}

	var info ytdlpInfo
	_ = json.Unmarshal(b, &info)
	norm := normalizeInfo(b)

	// Prefer the *input URL* as the stable src, rather than yt-dlp's webpage_url,
	// so embeds don't accidentally get re-attributed to the extractor's domain.
	rawJobURL := strings.TrimSpace(job.URL)
	src := rawJobURL
	canonicalDomain := ""
	expandedJobURL := ""
	if src != "" {
		if expanded, err := videoid.ExpandAndCanonicalizeURL(ctx, src); err == nil {
			expandedJobURL = expanded.ExpandedURL
			src = expandedJobURL
			canonicalDomain = expanded.CanonicalDomain
		}
		if normalized, canon2, err := videoid.NormalizeSourceURL(src); err == nil && strings.TrimSpace(normalized) != "" {
			src = normalized
			if canon2 != "" {
				canonicalDomain = canon2
			}
		}
	}

	// Fall back to yt-dlp URLs only if the job URL is missing.
	if src == "" {
		src = strings.TrimSpace(info.WebpageURL)
		if src == "" {
			src = strings.TrimSpace(info.OriginalURL)
		}
		if src != "" {
			if expanded, err := videoid.ExpandAndCanonicalizeURL(ctx, src); err == nil {
				src = expanded.ExpandedURL
				canonicalDomain = expanded.CanonicalDomain
			}
			if normalized, canon2, err := videoid.NormalizeSourceURL(src); err == nil && strings.TrimSpace(normalized) != "" {
				src = normalized
				if canon2 != "" {
					canonicalDomain = canon2
				}
			}
		}
	}
	if src == "" {
		src = filepath.Base(infoPath)
	}

	title := strings.TrimSpace(info.Title)
	if title == "" {
		title = src
	}

	var existing *db.Video
	{
		candidates := make([]string, 0, 8)
		seen := map[string]struct{}{}
		add := func(s string) {
			s = strings.TrimSpace(s)
			if s == "" {
				return
			}
			if _, ok := seen[s]; ok {
				return
			}
			seen[s] = struct{}{}
			candidates = append(candidates, s)
		}

		add(src)
		add(rawJobURL)
		add(expandedJobURL)
		add(strings.TrimSpace(info.WebpageURL))
		add(strings.TrimSpace(info.OriginalURL))

		// Include normalized variants for lookup.
		for _, s := range []string{rawJobURL, expandedJobURL, strings.TrimSpace(info.WebpageURL), strings.TrimSpace(info.OriginalURL)} {
			if normalized, _, nerr := videoid.NormalizeSourceURL(s); nerr == nil {
				add(normalized)
			}
		}

		for _, cand := range candidates {
			v, selErr := q.SelectVideoBySrc(ctx, cand)
			if selErr == nil {
				existing = v
				break
			}
			if !errors.Is(selErr, pgx.ErrNoRows) {
				return fmt.Errorf("select existing video: %w", selErr)
			}
		}
	}

	// If we found an existing row by any candidate, do not rewrite src here.
	// Rewriting src would require a dedicated migration/merge strategy.
	if existing != nil {
		src = existing.Src
	}

	// We are migrating to normalized `video_comments`; keep `videos.comments` empty to avoid
	// massive JSONB rows. Preserve existing `videos.comments` only to avoid destructive updates.
	comments := []byte("[]")
	if existing != nil {
		comments = existing.Comments
	}

	// Preserve existing permanent paths during refreshes; some refresh jobs only fetch metadata.
	var preservedVideoPath *string
	var preservedThumbPath *string
	if existing != nil {
		preservedVideoPath = existing.VideoPath
		preservedThumbPath = existing.ThumbnailPath
	}

	// Determine the video UUID.
	// - Refresh: keep the existing UUID (do not rewrite storage paths).
	// - New: deterministic UUIDv5 based on canonical domain + yt-dlp info.id.
	videoRowID := pgUUIDFromGoogle(uuid.New())
	if existing != nil {
		videoRowID = existing.ID
	} else {
		videoMetaID := strings.TrimSpace(info.ID)
		if videoMetaID != "" && canonicalDomain != "" {
			videoRowID = pgUUIDFromGoogle(videoid.VideoUUID(canonicalDomain, videoMetaID))
		}
	}

	gradStart, gradEnd, gradAngle := placeholderGradientForVideoID(videoRowID.String())
	videoArchivedBy := job.ArchivedBy
	if existing != nil {
		videoArchivedBy = existing.ArchivedBy
	}

	infoVI, err := videoinfo.NewVideoInfo(b)
	if err != nil {
		slog.Warn("failed to parse info JSON into VideoInfo", "error", err)
	}

	video, err := q.InsertVideo(ctx, &db.InsertVideoParams{
		ID:                 videoRowID,
		Src:                src,
		ArchivedBy:         videoArchivedBy,
		Title:              title,
		ThumbGradientStart: &gradStart,
		ThumbGradientEnd:   &gradEnd,
		ThumbGradientAngle: &gradAngle,
		Description:        norm.Description,
		Tags:               norm.Tags,
		Uploader:           norm.Uploader,
		UploaderID:         norm.UploaderID,
		ChannelID:          norm.ChannelID,
		UploadDate:         norm.UploadDate,
		DurationSeconds:    norm.DurationSeconds,
		ViewCount:          norm.ViewCount,
		LikeCount:          norm.LikeCount,
		Info:               infoVI,
		Comments:           comments,
		VideoPath:          preservedVideoPath,
		ThumbnailPath:      preservedThumbPath,
		FileHash:           nil,
		FileSize:           nil,
		ProbeData:          nil,
	})
	if err != nil {
		return fmt.Errorf("insert video: %w", err)
	}

	// Transcript ingest (best-effort). Intended for search.
	if job.SpoolDir != nil && strings.TrimSpace(*job.SpoolDir) != "" {
		capPath, lang, ok := findCaptionFilePath(infoPath, *job.SpoolDir)
		if ok {
			if err := ingestTranscriptFile(ctx, q, video.ID, lang, capPath); err != nil {
				slog.Warn("failed to ingest transcript", "video_id", video.ID, "path", capPath, "error", err)
			} else {
				slog.Info("Transcript ingested", "video_id", video.ID, "lang", lang)
			}
		}
	}

	// Comment ingest (best-effort). Extract from info.json "comments" array.
	commentSource := canonicalDomain
	if commentSource == "" {
		commentSource = "unknown"
	}
	if err := ingestCommentsFromInfoJSON(ctx, q, video.ID, commentSource, b); err != nil {
		slog.Warn("failed to ingest comments", "video_id", video.ID, "error", err)
	}

	// Asset generation/regeneration logic
	var videoPath *string
	var thumbPath *string
	var fileHash *string
	var fileSize *int64

	// FORMAT-SPECIFIC DOWNLOAD: When extra_args contains -f, this is a supplementary
	// format download (e.g. a specific quality chip). Don't overwrite the main video.
	// Instead, store the downloaded file alongside it and regenerate HLS.
	if isFormatSpecificDownload(job.ExtraArgs) && existing != nil && preservedVideoPath != nil && *preservedVideoPath != "" {
		slog.Info("format-specific download detected, merging without overwrite",
			"video_id", video.ID, "extra_args", job.ExtraArgs)

		if job.SpoolDir != nil && *job.SpoolDir != "" {
			if err := mergeFormatDownload(ctx, video.ID.String(), *job.SpoolDir, *preservedVideoPath); err != nil {
				slog.Error("failed to merge format download", "video_id", video.ID, "error", err)
			}
		}

		// Link the download job to the video
		if err := q.LinkDownloadJobVideo(ctx, &db.LinkDownloadJobVideoParams{ID: job.DownloadJobID, VideoID: video.ID}); err != nil {
			return fmt.Errorf("link download job video: %w", err)
		}
		return q.MarkIngestJobSucceeded(ctx, job.IngestJobID)
	}

	// Move files from spool to permanent storage
	if job.SpoolDir != nil && *job.SpoolDir != "" {
		videoPath, thumbPath, fileHash, fileSize, err = moveVideoToPermanentStorage(video.ID.String(), *job.SpoolDir)
		if err != nil {
			slog.Error("failed to move video to permanent storage", "video_id", video.ID, "error", err)
		}
	}

	// Preserve existing permanent paths if the spool dir didn't contain a new video/thumbnail,
	// or if this is a regeneration job (no spool dir)
	if videoPath == nil {
		videoPath = preservedVideoPath
	}
	if thumbPath == nil {
		thumbPath = preservedThumbPath
	}

	// If we have a video path (either from spool or existing), (re)generate all assets
	if videoPath != nil && *videoPath != "" {
		videoID := video.ID.String()
		slog.Info("generating video assets", "video_id", videoID, "video_path", *videoPath)

		// Always ensure we have a right-sized thumbnail (don't force regenerate on normal ingest).
		if p, genErr := generateVideoThumbnail(ctx, *videoPath, videoID, false); genErr != nil {
			slog.Warn("failed to generate thumbnail", "video_id", videoID, "error", genErr)
		} else {
			thumbPath = p
		}

		// Generate a lightweight hover preview (best-effort).
		if genErr := generateVideoPreview(ctx, *videoPath, videoID, false); genErr != nil {
			slog.Warn("failed to generate preview", "video_id", videoID, "error", genErr)
		}

		// Generate seek thumbnails (sprite sheets) (best-effort).
		if _, genErr := generateVideoSeekAssets(ctx, *videoPath, videoID, norm.DurationSeconds, false); genErr != nil {
			slog.Warn("failed to generate seek assets", "video_id", videoID, "error", genErr)
		}

		// Generate waveform peaks (best-effort).
		if _, genErr := generateVideoWaveform(ctx, *videoPath, videoID, norm.DurationSeconds, false); genErr != nil {
			slog.Warn("failed to generate waveform assets", "video_id", videoID, "error", genErr)
		}

		// Captions: if missing, optionally generate with Whisper and ingest transcript.
		dir := filepath.Dir(*videoPath)
		capPath, lang, ok := findCanonicalCaptionFilePath(dir, video.ID.String())
		if ok {
			if err := ingestTranscriptFile(ctx, q, video.ID, lang, capPath); err != nil {
				slog.Warn("failed to ingest transcript", "video_id", video.ID, "path", capPath, "error", err)
			} else {
				slog.Info("Transcript ingested", "video_id", video.ID, "lang", lang)
			}
		} else if whisperEnabled() {
			if p, l, err := generateCaptionsWithWhisper(ctx, *videoPath, video.ID.String(), dir); err != nil {
				slog.Warn("whisper caption generation failed", "video_id", video.ID, "error", err)
			} else {
				if err := ingestTranscriptFile(ctx, q, video.ID, l, p); err != nil {
					slog.Warn("failed to ingest whisper transcript", "video_id", video.ID, "path", p, "error", err)
				} else {
					slog.Info("Whisper transcript ingested", "video_id", video.ID, "lang", l)
				}
			}
		}

		// Run ffprobe to capture real stream metadata (best-effort).
		var probeInfo *videoinfo.ProbeInfo
		if probeResult, probeErr := ffmpeg.Probe(ctx, *videoPath); probeErr != nil {
			slog.Warn("failed to probe video", "video_id", videoID, "error", probeErr)
		} else {
			if pj, marshalErr := json.Marshal(probeResult.RawJSON); marshalErr == nil {
				probeInfo = videoinfo.NewProbeInfo(pj)
				slog.Info("ffprobe data captured",
					"video_id", videoID,
					"video_streams", probeResult.VideoStreams,
					"audio_streams", probeResult.AudioStreams,
					"codec", probeResult.VideoCodec,
				)
			}
		}

		// Generate HLS master playlist with separate audio tracks (best-effort).
		// This demuxes each stream, fragments into fMP4 segments, and writes master.m3u8.
		if probeResult, _ := ffmpeg.Probe(ctx, *videoPath); probeResult != nil && probeResult.AudioStreams > 1 {
			if _, hlsErr := generateHLS(ctx, *videoPath, videoID); hlsErr != nil {
				slog.Warn("failed to generate HLS", "video_id", videoID, "error", hlsErr)
			}
		}

		// Update video with paths (including regenerated assets)
		video, err = q.InsertVideo(ctx, &db.InsertVideoParams{
			ID:                 videoRowID,
			Src:                src,
			ArchivedBy:         videoArchivedBy,
			Title:              title,
			ThumbGradientStart: &gradStart,
			ThumbGradientEnd:   &gradEnd,
			ThumbGradientAngle: &gradAngle,
			Description:        norm.Description,
			Tags:               norm.Tags,
			Uploader:           norm.Uploader,
			UploaderID:         norm.UploaderID,
			ChannelID:          norm.ChannelID,
			UploadDate:         norm.UploadDate,
			DurationSeconds:    norm.DurationSeconds,
			ViewCount:          norm.ViewCount,
			LikeCount:          norm.LikeCount,
			Info:               infoVI,
			Comments:           comments,
			VideoPath:          videoPath,
			ThumbnailPath:      thumbPath,
			FileHash:           fileHash,
			FileSize:           fileSize,
			ProbeData:          probeInfo,
		})
		if err != nil {
			slog.Error("failed to update video with permanent paths", "video_id", video.ID, "error", err)
		}

		if err := updateVideoAssetsStatus(ctx, q, video.ID.String(), verifyAllAssetStatus(*videoPath, video.ID.String(), fileHash)); err != nil {
			slog.Warn("failed to update assets_status after ingest", "video_id", video.ID, "error", err)
		}
	}

	// Store a revision diff when refreshing an existing video.
	if existing != nil && job.Refresh {
		oldTitle := strings.TrimSpace(existing.Title)
		newTitle := strings.TrimSpace(video.Title)
		oldDesc := extractJSONString(existing.Info.RawJSON(), "description")
		newDesc := extractJSONString(b, "description")

		diff := map[string]any{}
		if oldTitle != newTitle {
			diff["title"] = map[string]string{"old": oldTitle, "new": newTitle}
		}
		if oldDesc != newDesc {
			diff["description"] = map[string]string{"old": oldDesc, "new": newDesc}
		}

		if len(diff) > 0 {
			diffJSON, _ := json.Marshal(diff)
			kind := "refresh"
			_ = q.InsertVideoRevision(ctx, &db.InsertVideoRevisionParams{
				VideoID:        video.ID,
				Kind:           kind,
				Diff:           diffJSON,
				OldTitle:       &oldTitle,
				NewTitle:       &newTitle,
				OldDescription: &oldDesc,
				NewDescription: &newDesc,
				OldInfo:        existing.Info.RawJSON(),
				NewInfo:        b,
			})
		}
	}

	if err := q.LinkDownloadJobVideo(ctx, &db.LinkDownloadJobVideoParams{ID: job.DownloadJobID, VideoID: video.ID}); err != nil {
		return fmt.Errorf("link download job video: %w", err)
	}

	return q.MarkIngestJobSucceeded(ctx, job.IngestJobID)
}

func listenAndSignal(ctx context.Context, dsn string, channel string, signalCh chan<- struct{}) {
	for {
		if ctx.Err() != nil {
			return
		}

		// Parse using pgxpool so pool_* DSN params are consumed client-side
		// (otherwise they get forwarded to Postgres as startup params and cause FATAL).
		poolConf, err := pgxpool.ParseConfig(dsn)
		if err != nil {
			slog.Error("listen parse config failed", "channel", channel, "error", err)
			time.Sleep(2 * time.Second)
			continue
		}

		conn, err := pgx.ConnectConfig(ctx, poolConf.ConnConfig)
		if err != nil {
			slog.Error("listen connect failed", "channel", channel, "error", err)
			time.Sleep(2 * time.Second)
			continue
		}

		q := db.New(conn)
		switch channel {
		case "ingest_jobs":
			err = q.ListenIngestJobs(ctx)
		default:
			err = fmt.Errorf("unsupported listen channel: %s", channel)
		}
		if err != nil {
			slog.Error("LISTEN failed", "channel", channel, "error", err)
			_ = conn.Close(ctx)
			time.Sleep(2 * time.Second)
			continue
		}

		for {
			if ctx.Err() != nil {
				_ = conn.Close(ctx)
				return
			}

			err := conn.PgConn().WaitForNotification(ctx)
			if err != nil {
				slog.Error("wait for notification failed", "channel", channel, "error", err)
				_ = conn.Close(ctx)
				break
			}

			select {
			case signalCh <- struct{}{}:
			default:
			}
		}
	}
}
