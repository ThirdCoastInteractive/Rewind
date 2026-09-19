package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/automarkers"
	rewindcomments "thirdcoast.systems/rewind/internal/comments"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/videoid"
	"thirdcoast.systems/rewind/pkg/ffmpeg"
	"thirdcoast.systems/rewind/pkg/videoinfo"
)

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
		dir := filepath.Join(downloadsDir(), videoID)
		files, _ := filepath.Glob(filepath.Join(dir, "*"))
		discovered := pickPreferredVideoPath(ctx, files)
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

	// Regenerate captions via rewind-ml transcribe job (whisper.cpp).
	if scope == "all" || scope == "captions" {
		if err := enqueueInteractiveTranscribeJob(ctx, q, videoRow.ID); err != nil {
			slog.Warn("enqueue transcribe ml job failed", "video_id", videoID, "error", err)
		} else {
			slog.Info("enqueued transcribe ml job", "video_id", videoID)
		}
	}

	// Refresh the alternate-quality streams manifest. (HLS has been removed —
	// playback is a direct stream of the normalized MP4, and quality variants are
	// offered as direct alternate <source> files.)
	if scope == "all" || scope == "streams" {
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

// ingestMediaKind is "metadata" only for skip-download jobs that still have no
// video file. Any real path (or a later full download of the same src) is "file".
func ingestMediaKind(jobKind string, videoPath *string) string {
	if videoPath != nil && strings.TrimSpace(*videoPath) != "" {
		return "file"
	}
	if strings.TrimSpace(jobKind) == "metadata" {
		return "metadata"
	}
	return "file"
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
	// Live sessions override this below so channel pages are not reused across streams.
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

	src, candidates := resolveVideosSrc(videosSrcInput{
		RawJobURL:       rawJobURL,
		ExpandedJobURL:  expandedJobURL,
		JobDerivedSrc:   src,
		WebpageURL:      strings.TrimSpace(info.WebpageURL),
		OriginalURL:     strings.TrimSpace(info.OriginalURL),
		InfoID:          strings.TrimSpace(info.ID),
		CanonicalDomain: canonicalDomain,
		LiveStatus:      info.LiveStatus,
		IsLive:          info.IsLive,
		WasLive:         info.WasLive,
	})
	if src != "" {
		if _, canon2, err := videoid.NormalizeSourceURL(src); err == nil && canon2 != "" {
			canonicalDomain = canon2
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
		Media:              ingestMediaKind(job.Kind, preservedVideoPath),
	})
	if err != nil {
		return fmt.Errorf("insert video: %w", err)
	}
	attachChannel(ctx, q, video, src, infoVI)
	automarkers.IngestFromInfoJSON(ctx, q, video, b)

	// Transcript ingest (best-effort). Intended for search.
	subtitleState := "pending"
	subtitleError := ""
	if job.SpoolDir != nil && strings.TrimSpace(*job.SpoolDir) != "" {
		capPath, lang, ok := findCaptionFilePath(infoPath, *job.SpoolDir)
		if ok {
			if err := ingestTranscriptFile(ctx, q, video.ID, lang, capPath); err != nil {
				slog.Warn("failed to ingest transcript", "video_id", video.ID, "path", capPath, "error", err)
				subtitleState, subtitleError = "failed", err.Error()
			} else {
				slog.Info("Transcript ingested", "video_id", video.ID, "lang", lang)
				subtitleState = subtitleKindFromInfo(b)
			}
		} else if strings.TrimSpace(job.Kind) == "metadata" {
			failurePath := filepath.Join(*job.SpoolDir, "subtitle-fetch.error")
			if failure, err := os.ReadFile(failurePath); err == nil && strings.TrimSpace(string(failure)) != "" {
				subtitleState, subtitleError = "failed", strings.TrimSpace(string(failure))
				_ = os.Remove(failurePath)
			} else {
				subtitleState = "unavailable"
			}
		}
	}
	if subtitleState != "pending" {
		if err := q.SetVideoSubtitleState(ctx, &db.SetVideoSubtitleStateParams{ID: video.ID, SubtitleState: subtitleState, LastError: subtitleError}); err != nil {
			slog.Warn("failed to set subtitle state", "video_id", video.ID, "error", err)
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
	if job.SpoolDir != nil && strings.TrimSpace(*job.SpoolDir) != "" {
		if chatPath := findLiveChatFile(*job.SpoolDir); chatPath != "" {
			raw, rerr := os.ReadFile(chatPath)
			if rerr != nil {
				slog.Warn("read live chat json", "path", chatPath, "error", rerr)
			} else if err := rewindcomments.IngestLiveChatJSON(ctx, q, video.ID, raw); err != nil {
				slog.Warn("failed to ingest live chat", "video_id", video.ID, "error", err)
			}
		}
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

	// Keep retries attached to this row even if the spool move is interrupted.
	if err := q.LinkDownloadJobVideo(ctx, &db.LinkDownloadJobVideoParams{ID: job.DownloadJobID, VideoID: video.ID}); err != nil {
		return fmt.Errorf("link download job before moving media: %w", err)
	}

	// Move files from spool to permanent storage
	if job.SpoolDir != nil && *job.SpoolDir != "" {
		videoPath, thumbPath, fileHash, fileSize, err = moveVideoToPermanentStorage(ctx, video.ID.String(), *job.SpoolDir)
		if err != nil {
			return fmt.Errorf("move video to permanent storage: %w", err)
		}
	}

	// Metadata-only jobs must never write a video_path (thumbnails/info.json are fine).
	if strings.TrimSpace(job.Kind) == "metadata" {
		videoPath = preservedVideoPath
		fileHash = nil
		fileSize = nil
	}

	// Preserve existing permanent paths if the spool dir didn't contain a new video/thumbnail,
	// or if this is a regeneration job (no spool dir)
	if videoPath == nil {
		videoPath = preservedVideoPath
	}
	if thumbPath == nil {
		thumbPath = preservedThumbPath
	}

	// The source file is durable now. Publish playback here; preview/seek/waveform
	// run on the asset worker pool so this ingest job can return.
	if videoPath != nil && strings.TrimSpace(*videoPath) != "" {
		if err := publishIngestMedia(ctx, q, video.ID, *videoPath, thumbPath, fileHash); err != nil {
			return fmt.Errorf("publish stored media: %w", err)
		}
	} else if strings.TrimSpace(job.Kind) != "metadata" {
		return fmt.Errorf("download has no source video in permanent storage")
	}
	if err := q.LinkDownloadJobVideo(ctx, &db.LinkDownloadJobVideoParams{ID: job.DownloadJobID, VideoID: video.ID}); err != nil {
		return fmt.Errorf("link download job before publish: %w", err)
	}

	// If we have a video path, finish cheap ingest work and hand derived assets
	// (preview/seek/waveform) to the asset worker pool so this job can return.
	if videoPath != nil && *videoPath != "" {
		videoID := video.ID.String()
		slog.Info("publishing ingest media", "video_id", videoID, "video_path", *videoPath)

		// Library cards need a thumbnail; a single JPEG is not long-tail generation.
		if p, genErr := generateVideoThumbnail(ctx, *videoPath, videoID, false); genErr != nil {
			slog.Warn("failed to generate thumbnail", "video_id", videoID, "error", genErr)
		} else {
			thumbPath = p
		}

		// Captions already on disk are cheap to ingest. Whisper is an ML job.
		dir := filepath.Dir(*videoPath)
		capPath, lang, ok := findCanonicalCaptionFilePath(dir, video.ID.String())
		if ok {
			if err := ingestTranscriptFile(ctx, q, video.ID, lang, capPath); err != nil {
				slog.Warn("failed to ingest transcript", "video_id", video.ID, "path", capPath, "error", err)
			} else {
				slog.Info("Transcript ingested", "video_id", video.ID, "lang", lang)
			}
		} else if p, l, restored := materializeStoredTranscript(ctx, q, video.ID, dir, video.ID.String()); restored {
			slog.Info("restored captions from stored transcript", "video_id", video.ID, "lang", l, "path", p)
		} else if err := enqueueTranscribeJob(ctx, q, video.ID); err != nil {
			slog.Warn("enqueue transcribe ml job failed", "video_id", video.ID, "error", err)
		} else {
			slog.Info("enqueued transcribe ml job", "video_id", video.ID)
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

		// Persist permanent paths now that the file is in the archive.
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
			Media:              ingestMediaKind(job.Kind, videoPath),
		})
		if err != nil {
			return fmt.Errorf("update video with permanent paths: %w", err)
		} else {
			attachChannel(ctx, q, video, src, infoVI)
		}

		if err := updateVideoAssetsStatus(ctx, q, video.ID.String(), verifyAllAssetStatus(*videoPath, video.ID.String(), fileHash)); err != nil {
			slog.Warn("failed to update assets_status after ingest", "video_id", video.ID, "error", err)
		}
	} else if thumbPath != nil && strings.TrimSpace(*thumbPath) != "" {
		if err := q.UpdateVideoThumbnailPath(ctx, &db.UpdateVideoThumbnailPathParams{ID: video.ID, ThumbnailPath: thumbPath}); err != nil {
			slog.Warn("failed to update thumbnail path", "video_id", video.ID, "error", err)
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

	if strings.TrimSpace(job.Kind) != "metadata" && videoPath != nil && strings.TrimSpace(*videoPath) != "" {
		enqueuePostIngestAssets(ctx, q, video.ID, job.DownloadJobID, job.IngestJobID)
	}

	return q.MarkIngestJobSucceeded(ctx, job.IngestJobID)
}

func shouldEnqueuePostIngestAssets(active []*db.GetActiveAssetJobsForVideoRow, current pgtype.UUID) bool {
	for _, job := range active {
		if job == nil {
			continue
		}
		if job.IngestJobID != current {
			return false
		}
	}
	return true
}

func enqueuePostIngestAssets(ctx context.Context, q *db.Queries, videoID, downloadJobID, ingestJobID pgtype.UUID) {
	active, err := q.GetActiveAssetJobsForVideo(ctx, videoID)
	if err != nil {
		slog.Warn("failed to list active asset jobs", "video_id", videoID, "error", err)
		return
	}
	if !shouldEnqueuePostIngestAssets(active, ingestJobID) {
		slog.Info("post-ingest assets already queued", "video_id", videoID)
		return
	}
	job, err := q.EnqueuePostIngestAssetsJob(ctx, downloadJobID)
	if err != nil {
		slog.Warn("failed to enqueue post-ingest asset generation", "video_id", videoID, "error", err)
		return
	}
	slog.Info("enqueued post-ingest asset generation", "video_id", videoID, "ingest_job_id", job.ID)
}
