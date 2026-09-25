package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/transcription"
	"thirdcoast.systems/rewind/pkg/captions"
	"thirdcoast.systems/rewind/pkg/ffmpeg"
	"thirdcoast.systems/rewind/pkg/plugin"
)

func handleTranscribe(ctx context.Context, dbc *db.DatabaseConnection, job *db.MlJob, downloadsDir string) error {
	q := dbc.Queries(ctx)
	video, err := q.GetVideoByID(ctx, job.VideoID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("waiting_assets: video not found")
		}
		return fmt.Errorf("video: %w", err)
	}
	if video == nil {
		return fmt.Errorf("waiting_assets: video not found")
	}
	videoID := uuidString(video.ID)
	videoPath, cleanup := resolveVideoPath(ctx, video, downloadsDir)
	if cleanup != nil {
		defer cleanup()
	}
	if videoPath == "" {
		return fmt.Errorf("waiting_assets: no video file for %s", videoID)
	}
	if job.RepairTranscript {
		return handleTranscriptRepair(ctx, dbc, job, videoPath)
	}
	dir := filepath.Dir(videoPath)
	if video.DurationSeconds == nil || *video.DurationSeconds <= 0 {
		probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		duration, probeErr := ffmpeg.ProbeDuration(probeCtx, videoPath)
		cancel()
		if probeErr == nil && duration > 0 && duration < math.MaxInt32 {
			seconds := int32(math.Ceil(duration))
			_ = q.RepairVideoDuration(ctx, &db.RepairVideoDurationParams{ID: video.ID, DurationSeconds: &seconds})
		}
	}
	var bounds []float64
	needScratch := isHTTPURL(videoPath)
	if job.RangeStart != nil && job.RangeEnd != nil {
		bounds = []float64{*job.RangeStart, *job.RangeEnd}
		needScratch = true
	}
	if needScratch {
		var err error
		dir, err = os.MkdirTemp("", "rewind-asr-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir) // Only this newly allocated scratch directory.
	}
	cfg := loadWhisperConfig(ctx)
	if job.PromptVersion == transcription.CPUPromptVersion {
		cfg.Device = "cpu"
	}
	capPath, lang, wErr := generateCaptionsWithWhisperConfig(ctx, videoPath, videoID, dir, cfg, bounds...)
	if wErr != nil {
		return wErr
	}
	q, tx, err := dbc.NewWithTX(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = q.LockMLPublication(ctx, &db.LockMLPublicationParams{ID: job.ID, LeaseToken: job.LeaseToken}); err != nil {
		return leaseLost(err)
	}
	if len(bounds) == 2 {
		doc, err := captions.CleanFile(capPath)
		if err != nil {
			return err
		}
		if err := transcription.StoreRange(ctx, q, video.ID, lang, doc.Cues, bounds[0], bounds[1]); err != nil {
			return err
		}
	} else if err := ingestTranscriptFile(ctx, q, video.ID, lang, capPath); err != nil {
		return fmt.Errorf("ingest transcript: %w", err)
	}
	slog.Info("transcribe ingested captions", "video_id", videoID, "lang", lang, "path", capPath)
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if e := maybeEnqueueDiarize(ctx, dbc, video.ID); e != nil {
		slog.Warn("enqueue diarize", "video_id", videoID, "error", e)
	}
	return nil
}

var videoExts = []string{".mp4", ".webm", ".mkv", ".mov", ".avi"}

func isHTTPURL(s string) bool {
	low := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://")
}

// resolveVideoPath order: videos.video_path → plugin.LocalMaster → downloadsDir
// globs → plugin.MasterSource (local path or http(s) for ffmpeg -i).
func resolveVideoPath(ctx context.Context, v *db.Video, downloadsDir string) (string, func()) {
	if v.VideoPath != nil {
		p := strings.TrimSpace(*v.VideoPath)
		if p != "" {
			if _, err := os.Stat(p); err == nil {
				return p, nil
			}
			// In Live, VideoPath may be the private R2 object key. Once remote
			// media is configured, an invalid key must fail closed rather than
			// falling through to an unrelated local or public source.
			if os.Getenv("R2_PUBLIC_URL") != "" || os.Getenv("R2_SIGNING_SECRET") != "" {
				if src, err := configuredPrivateMasterURL(p, time.Now()); err == nil {
					return src, nil
				}
				return "", nil
			}
		}
	}
	id := uuidString(v.ID)
	if p, ok := plugin.LocalMaster(id); ok {
		return p, nil
	}
	dir := filepath.Join(downloadsDir, id)
	for _, ext := range videoExts {
		p := filepath.Join(dir, id+".video"+ext)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	matches, _ := filepath.Glob(filepath.Join(dir, id+".video.*"))
	for _, p := range matches {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	src, cleanup, err := plugin.MasterSource(ctx, id)
	if err != nil || strings.TrimSpace(src) == "" {
		if cleanup != nil {
			cleanup()
		}
		return "", nil
	}
	src = strings.TrimSpace(src)
	// extractWav16k shells ffmpeg -i, which accepts http(s); pass through.
	return src, cleanup
}
