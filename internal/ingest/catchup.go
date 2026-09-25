package ingest

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/captions"
	"thirdcoast.systems/rewind/pkg/ffmpeg"
	"thirdcoast.systems/rewind/pkg/videoinfo"
)

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

// recoverOrphanedVideoPaths resolves videos whose video_path is unset — ingests
// that never completed, leaving a file on disk but no DB path (so the catchup
// loop, which requires a non-null path, skips them forever).
//
// For each, it scans the canonical /downloads/<id>/ directory:
//   - If a readable (ffprobe-validated) video file is found, its path is persisted
//     so the asset-catchup loop will canonicalize and normalize it.
//   - Unprobeable files are retained for later recovery; a probe failure must
//     never destroy archive content.
func recoverOrphanedVideoPaths(ctx context.Context, dbc *db.DatabaseConnection) {
	q := dbc.Queries(ctx)
	ids, err := q.ListVideosMissingVideoPath(ctx, 500)
	if err != nil {
		slog.Warn("orphan path recovery query failed", "error", err)
		return
	}
	if len(ids) == 0 {
		return
	}
	slog.Info("orphan video path recovery start", "candidates", len(ids))

	recovered, unreadable, skipped := 0, 0, 0
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		dir := filepath.Join(downloadsDir(), id)
		files, _ := filepath.Glob(filepath.Join(dir, "*"))

		// Collect candidate source video files (exclude derived hover previews).
		var videoFiles []string
		for _, f := range files {
			if !isVideoExt(strings.ToLower(filepath.Ext(f))) {
				continue
			}
			base := strings.ToLower(filepath.Base(f))
			if base == "preview.mp4" || strings.Contains(base, ".preview.") {
				continue
			}
			videoFiles = append(videoFiles, f)
		}
		if len(videoFiles) == 0 {
			skipped++
			continue // nothing on disk to recover
		}

		// Partition into readable vs broken via ffprobe.
		var readable, broken []string
		for _, vf := range videoFiles {
			if isReadableVideoFile(ctx, vf) {
				readable = append(readable, vf)
			} else {
				broken = append(broken, vf)
			}
		}

		if len(readable) > 0 {
			candidate := pickPreferredVideoPath(ctx, readable)
			if candidate == "" {
				candidate = readable[0]
			}
			var idUUID pgtype.UUID
			if err := idUUID.Scan(id); err != nil {
				continue
			}
			if err := q.UpdateVideoPath(ctx, &db.UpdateVideoPathParams{ID: idUUID, VideoPath: &candidate}); err != nil {
				slog.Warn("orphan path recovery: failed to set video_path", "video_id", id, "error", err)
				continue
			}
			slog.Info("orphan path recovery: resolved video_path from disk", "video_id", id, "path", candidate)
			recovered++
			continue
		}

		for _, path := range broken {
			slog.Warn("orphan path recovery: retaining unprobeable video", "video_id", id, "path", path)
			unreadable++
		}
	}
	slog.Info("orphan video path recovery complete",
		"recovered", recovered, "unprobeable_retained", unreadable, "skipped_no_file", skipped)
}

// runAssetCatchupUnit performs a small amount of asset backfill work (best-effort).
// It queries for videos that are actually missing assets (incomplete assets_status)
// and processes a small batch. Successfully processed videos update their assets_status
// and drop out of future queries. Errors are collected per-asset and stored in
// assets_status so they can be surfaced to admins.
func runAssetCatchupUnit(ctx context.Context, dbc *db.DatabaseConnection) int {
	const maxVideos = 8
	processed := 0
	q := dbc.Queries(ctx)

	rows, err := q.ListVideosForAssetCatchup(ctx, int32(maxVideos))
	if err != nil {
		slog.Warn("asset catchup unit query failed", "error", err)
		return 0
	}
	if len(rows) == 0 {
		return 0
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

		if videoPath == "" {
			continue
		}
		// Imported Live masters are durable R2 keys. Filesystem catchup cannot
		// canonicalize them and must never replace the authoritative key with a
		// transient spool path.
		if isPrivateMasterKey(videoPath) {
			var idUUID pgtype.UUID
			if err := idUUID.Scan(videoID); err != nil {
				continue
			}
			slog.Info("asset catchup scheduling remote master", "video_id", videoID)
			enqueueCatchupAssets(ctx, q, idUUID)
			continue
		}

		slog.Info("asset catchup scan", "video_id", videoID, "video_path", videoPath, "thumb_path", derefString(thumbPath), "has_hash", fileHash != nil && strings.TrimSpace(*fileHash) != "", "duration_seconds", row.DurationSeconds)

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
		migratedVideoPath, migratedThumbPath, _ := migrateVideoAssetsToCanonicalDir(ctx, videoID, videoPath, thumbPath)
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
			// Faststart: repair MP4 moov atom position for instant browser seeking.
			// Stream-copy only — no re-encoding, no quality loss.
			if strings.ToLower(filepath.Ext(videoPath)) == ".mp4" && !mp4HasFaststart(videoPath) {
				slog.Info("asset catchup: applying faststart to existing MP4", "video_id", videoID)
				if err := ffmpeg.ApplyFaststart(ctx, videoPath); err != nil {
					slog.Warn("asset catchup: faststart failed", "video_id", videoID, "error", err)
					assetErrors["faststart"] = err.Error()
				}
			}

			// Thumbnail: find existing or generate
			if p, err := generateVideoThumbnail(ctx, videoPath, filepath.Dir(videoPath), videoID, false); err == nil {
				_ = q.UpdateVideoThumbnailPath(ctx, &db.UpdateVideoThumbnailPathParams{ID: idUUID, ThumbnailPath: p})
			} else {
				slog.Warn("asset catchup thumbnail failed", "video_id", videoID, "error", err)
				assetErrors["thumbnail"] = err.Error()
			}

			if derivedAssetsIncomplete(verifyAllAssetStatus(videoPath, videoID, fileHash)) {
				enqueueCatchupAssets(ctx, q, idUUID)
			}

			// Ensure the canonical video is a browser-playable, faststart MP4.
			// (Replaces the old HLS demux/transcode pipeline — playback is now a
			// direct stream of a normalized MP4.)
			if normalized, nErr := ensureStreamableMP4(ctx, videoPath); nErr != nil {
				slog.Warn("asset catchup normalize failed", "video_id", videoID, "error", nErr)
				assetErrors["video_normalize"] = nErr.Error()
			} else if normalized != videoPath {
				videoPath = normalized
				_ = q.UpdateVideoPath(ctx, &db.UpdateVideoPathParams{ID: idUUID, VideoPath: &videoPath})
			}

			// Captions: find existing or generate via Whisper
			if capPath, lang, ok := findCanonicalCaptionFilePath(filepath.Dir(videoPath), videoID); ok {
				if iErr := ingestTranscriptFile(ctx, q, idUUID, lang, capPath); iErr != nil {
					slog.Warn("asset catchup transcript ingest failed", "video_id", videoID, "error", iErr)
					assetErrors["captions"] = iErr.Error()
				}
			} else if p, l, restored := materializeStoredTranscript(ctx, q, idUUID, filepath.Dir(videoPath), videoID); restored {
				slog.Info("asset catchup restored captions from stored transcript", "video_id", videoID, "lang", l, "path", p)
			} else if err := enqueueTranscribeJob(ctx, q, idUUID); err != nil {
				slog.Warn("asset catchup enqueue transcribe failed", "video_id", videoID, "error", err)
				assetErrors["captions"] = err.Error()
			} else {
				slog.Info("asset catchup enqueued transcribe ml job", "video_id", videoID)
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
			return processed
		case <-time.After(10 * time.Millisecond):
		}
	}

	if processed > 0 {
		slog.Info("asset catchup unit complete", "videos_scanned", processed)
	}
	return processed
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
func generateVideoThumbnail(ctx context.Context, ffmpegSrc, outDir, videoID string, forceRegenerate bool) (*string, error) {
	thumbPath := filepath.Join(outDir, videoID+".thumbnail.jpg")

	if forceRegenerate {
		if err := os.Remove(thumbPath); err != nil && !os.IsNotExist(err) {
			slog.Warn("failed to delete existing thumbnail", "path", thumbPath, "error", err)
		} else if err == nil {
			slog.Info("deleted existing thumbnail for regeneration", "path", thumbPath)
		}
		for _, variant := range thumbnailVariants {
			variantPath := thumbnailVariantPath(outDir, videoID, variant.Label)
			if err := os.Remove(variantPath); err != nil && !os.IsNotExist(err) {
				slog.Warn("failed to delete existing thumbnail variant", "path", variantPath, "error", err)
			} else if err == nil {
				slog.Info("deleted existing thumbnail variant for regeneration", "path", variantPath)
			}
		}
	}

	p, err := generateThumbnail(ctx, ffmpegSrc, outDir, videoID)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// generateVideoPreview generates a preview MP4 for a video, optionally deleting the existing one first.
func generateVideoPreview(ctx context.Context, ffmpegSrc, outDir, videoID string, forceRegenerate bool) error {
	previewPath := filepath.Join(outDir, videoID+".preview.mp4")

	if forceRegenerate {
		if err := os.Remove(previewPath); err != nil && !os.IsNotExist(err) {
			slog.Warn("failed to delete existing preview", "path", previewPath, "error", err)
		} else if err == nil {
			slog.Info("deleted existing preview for regeneration", "path", previewPath)
		}
	}

	return generatePreviewMP4(ctx, ffmpegSrc, outDir, videoID)
}

// generateVideoSeekAssets generates seek sprite sheets for a video, optionally deleting existing ones first.
func generateVideoSeekAssets(ctx context.Context, ffmpegSrc, outDir, videoID string, durationSeconds *int32, forceRegenerate bool) (bool, error) {
	if forceRegenerate {
		seekDir := filepath.Join(outDir, "seek")
		if err := os.RemoveAll(seekDir); err != nil && !os.IsNotExist(err) {
			slog.Warn("failed to delete existing seek directory", "path", seekDir, "error", err)
		} else if err == nil {
			slog.Info("deleted existing seek directory for regeneration", "path", seekDir)
		}
	}

	return ensureSeekAssets(ctx, ffmpegSrc, outDir, durationSeconds)
}

// generateVideoWaveform generates waveform data for a video, optionally deleting existing data first.
func generateVideoWaveform(ctx context.Context, ffmpegSrc, outDir, videoID string, durationSeconds *int32, forceRegenerate bool) (bool, error) {
	if forceRegenerate {
		waveformDir := filepath.Join(outDir, "waveform")
		if err := os.RemoveAll(waveformDir); err != nil && !os.IsNotExist(err) {
			slog.Warn("failed to delete existing waveform directory", "path", waveformDir, "error", err)
		} else if err == nil {
			slog.Info("deleted existing waveform directory for regeneration", "path", waveformDir)
		}
	}

	return ensureWaveformAssets(ctx, ffmpegSrc, outDir, durationSeconds)
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
	capPath, _, capOK := findCanonicalCaptionFilePath(dir, videoID)
	status["captions"] = capOK
	if !capOK {
		status["captions_clean"] = true
	} else if raw, err := os.ReadFile(capPath); err == nil {
		status["captions_clean"] = !captions.LooksDirty(raw)
	} else {
		status["captions_clean"] = false
	}

	// Faststart: MP4 moov atom at front for instant browser seek.
	// Non-MP4 formats (WebM, MKV) don't use this structure, mark as N/A (true).
	if strings.ToLower(filepath.Ext(videoPath)) == ".mp4" {
		status["faststart"] = mp4HasFaststart(videoPath)
	} else {
		status["faststart"] = true
	}

	return status
}

func derivedAssetsIncomplete(status map[string]any) bool {
	if preview, _ := status["preview"].(bool); !preview {
		return true
	}
	if waveform, _ := status["waveform"].(bool); !waveform {
		return true
	}
	switch seek := status["seek"].(type) {
	case bool:
		return !seek
	case map[string]bool:
		if len(seek) == 0 {
			return true
		}
		for _, ok := range seek {
			if !ok {
				return true
			}
		}
		return false
	default:
		return true
	}
}

// runRemoteMasterCatchup queues derived assets for R2 masters. The filesystem
// catchup query ignores org/ keys so it cannot rewrite them to a spool path.
func runRemoteMasterCatchup(ctx context.Context, dbc *db.DatabaseConnection) {
	rows, err := dbc.Query(ctx, `
		SELECT id::text
		FROM videos
		WHERE video_path LIKE 'org/%'
		  AND (thumbnail_path IS NULL OR btrim(thumbnail_path) = '')
		ORDER BY updated_at ASC
		LIMIT 8`)
	if err != nil {
		slog.Warn("remote master catchup query failed", "error", err)
		return
	}
	defer rows.Close()
	q := dbc.Queries(ctx)
	n := 0
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			slog.Warn("remote master catchup scan failed", "error", err)
			return
		}
		var idUUID pgtype.UUID
		if err := idUUID.Scan(id); err != nil {
			continue
		}
		enqueueCatchupAssets(ctx, q, idUUID)
		n++
	}
	if err := rows.Err(); err != nil {
		slog.Warn("remote master catchup rows failed", "error", err)
	}
	if n > 0 {
		slog.Info("remote master catchup", "videos", n)
	}
}

func enqueueCatchupAssets(ctx context.Context, q *db.Queries, videoID pgtype.UUID) {
	active, err := q.GetActiveAssetJobsForVideo(ctx, videoID)
	if err != nil {
		slog.Warn("asset catchup: list active asset jobs failed", "video_id", videoID, "error", err)
		return
	}
	if !shouldEnqueuePostIngestAssets(active, pgtype.UUID{}) {
		slog.Info("asset catchup: generation already queued", "video_id", videoID)
		return
	}
	if _, err := q.EnqueueAssetRegenerationJob(ctx, &db.EnqueueAssetRegenerationJobParams{VideoID: videoID}); err != nil {
		slog.Warn("asset catchup: enqueue generation failed", "video_id", videoID, "error", err)
		return
	}
	slog.Info("asset catchup enqueued generation", "video_id", videoID)
}

// mp4HasFaststart reports whether an MP4 file's moov atom appears before the
// first mdat atom.  When true, browsers can seek without buffering the file.
func mp4HasFaststart(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, 8)
	for {
		if _, err := io.ReadFull(f, buf); err != nil {
			return false
		}
		size := uint64(buf[0])<<24 | uint64(buf[1])<<16 | uint64(buf[2])<<8 | uint64(buf[3])
		atomType := string(buf[4:8])

		if atomType == "moov" {
			return true
		}
		if atomType == "mdat" {
			return false
		}

		// Extended size: size==1 means 8-byte size follows immediately.
		if size == 1 {
			var extBuf [8]byte
			if _, err := io.ReadFull(f, extBuf[:]); err != nil {
				return false
			}
			size = uint64(extBuf[0])<<56 | uint64(extBuf[1])<<48 | uint64(extBuf[2])<<40 | uint64(extBuf[3])<<32 |
				uint64(extBuf[4])<<24 | uint64(extBuf[5])<<16 | uint64(extBuf[6])<<8 | uint64(extBuf[7])
			if size < 16 {
				return false
			}
			if _, err := f.Seek(int64(size-16), io.SeekCurrent); err != nil {
				return false
			}
		} else {
			if size < 8 {
				return false
			}
			if _, err := f.Seek(int64(size-8), io.SeekCurrent); err != nil {
				return false
			}
		}
	}
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
