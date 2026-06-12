package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"thirdcoast.systems/rewind/pkg/ffmpeg"
)

// moveVideoToPermanentStorage moves video files from spool to /downloads/{videoID}/
// Returns: videoPath, thumbnailPath, fileHash, fileSize, error
func moveVideoToPermanentStorage(ctx context.Context, videoID, spoolDir string) (*string, *string, *string, *int64, error) {
	// Create permanent directory: /downloads/{videoID}/
	permanentDir := filepath.Join("/downloads", videoID)
	if err := os.MkdirAll(permanentDir, 0755); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("create permanent directory: %w", err)
	}

	// Find all files in spool directory
	files, err := filepath.Glob(filepath.Join(spoolDir, "*"))
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("glob spool files: %w", err)
	}

	preferredVideoSrcPath := pickPreferredVideoPath(ctx, files)
	preferredImageSrcPath := pickPreferredImagePath(files)

	var videoPath *string
	var thumbnailPath *string

	// Move each file to permanent storage
	for _, srcPath := range files {
		filename := filepath.Base(srcPath)
		ext := strings.ToLower(filepath.Ext(filename))

		isVideo := isVideoExt(ext)
		if isVideo && preferredVideoSrcPath != "" && srcPath != preferredVideoSrcPath {
			// yt-dlp can leave both the original container (e.g. .webm) and the remuxed file (e.g. .mkv).
			// We only want one canonical video in permanent storage.
			if err := os.Remove(srcPath); err != nil {
				slog.Warn("failed to remove extra video file", "path", srcPath, "error", err)
			}
			continue
		}
		isImage := ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".webp"
		if isImage && preferredImageSrcPath != "" && srcPath != preferredImageSrcPath {
			// Keep only one "source thumbnail" file from yt-dlp to avoid duplicating artwork.
			if err := os.Remove(srcPath); err != nil {
				slog.Warn("failed to remove extra image file", "path", srcPath, "error", err)
			}
			continue
		}

		destFilename := destFilenameForIngestAsset(videoID, filename)
		if strings.TrimSpace(destFilename) == "" {
			suffix := suffixFromFirstDot(filename)
			if suffix == "" {
				suffix = ext
			}
			destFilename = videoID + suffix
		}
		destPath := filepath.Join(permanentDir, destFilename)

		// Move file (rename is fastest)
		if err := os.Rename(srcPath, destPath); err != nil {
			// If rename fails (cross-device), copy and delete
			if err := copyFile(srcPath, destPath); err != nil {
				slog.Warn("failed to move file", "src", srcPath, "dest", destPath, "error", err)
				continue
			}
			_ = os.Remove(srcPath)
		}

		// Track video and thumbnail paths
		if videoPath == nil && isVideo {
			videoPath = &destPath
		} else if thumbnailPath == nil && strings.Contains(strings.ToLower(destFilename), ".thumbnail") {
			thumbnailPath = &destPath
		}
	}

	// Clean up spool directory (best-effort).
	_ = os.RemoveAll(spoolDir)

	// Normalize to a single browser-playable, faststart MP4. yt-dlp already remuxes
	// to mp4 in the common case, so this is usually just a faststart. When it fell
	// back to mkv (incompatible audio/subtitles) or this is a raw upload, the source
	// is converted in one pass — video copied unless legacy, audio transcoded to AAC
	// only when unplayable — and the verified-redundant source is removed.
	if videoPath != nil {
		if normalized, err := ensureStreamableMP4(ctx, *videoPath); err != nil {
			slog.Warn("normalize to streamable mp4 failed (keeping original)", "path", *videoPath, "error", err)
		} else {
			videoPath = &normalized
		}
	}

	// Compute SHA256 hash and size of video file if present
	var fileHash *string
	var fileSize *int64
	if videoPath != nil {
		if h, s, err := computeFileHashAndSize(*videoPath); err == nil {
			fileHash = &h
			fileSize = &s
		} else {
			slog.Warn("failed to compute file hash", "path", *videoPath, "error", err)
		}
	}

	return videoPath, thumbnailPath, fileHash, fileSize, nil
}

func destFilenameForIngestAsset(videoID string, srcFilename string) string {
	srcFilename = strings.TrimSpace(srcFilename)
	if srcFilename == "" {
		return ""
	}
	lower := strings.ToLower(srcFilename)

	// ytdlp metadata
	if strings.HasSuffix(lower, ".info.json") {
		return videoID + ".info.json"
	}

	// Captions/subtitles
	if strings.HasSuffix(lower, ".vtt") {
		lang := "und"
		// Common yt-dlp naming: <base>.<lang>.vtt
		parts := strings.Split(lower, ".")
		if len(parts) >= 2 {
			cand := parts[len(parts)-2]
			if cand != "" && cand != "vtt" {
				lang = cand
			}
		}
		return videoID + ".captions." + lang + ".vtt"
	}

	// Video file
	ext := strings.ToLower(filepath.Ext(srcFilename))
	if isVideoExt(ext) {
		return videoID + ".video" + ext
	}

	// Source thumbnail (downloaded artwork) - keep original extension
	if ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".webp" {
		return videoID + ".src_thumbnail" + ext
	}

	return ""
}

func pickPreferredImagePath(paths []string) string {
	bestPath := ""
	var bestSize int64 = -1
	for _, p := range paths {
		ext := strings.ToLower(filepath.Ext(p))
		if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".webp" {
			continue
		}
		sz := int64(-1)
		if fi, err := os.Stat(p); err == nil {
			sz = fi.Size()
		}
		if bestPath == "" || sz > bestSize {
			bestPath = p
			bestSize = sz
		}
	}
	return bestPath
}

func moveOrCopyFile(srcPath, destPath string) error {
	if err := os.Rename(srcPath, destPath); err == nil {
		return nil
	}
	if err := copyFile(srcPath, destPath); err != nil {
		return err
	}
	_ = os.Remove(srcPath)
	return nil
}

func migrateVideoDirAssets(ctx context.Context, videoID string, videoPath string, thumbnailPath *string) (string, *string, error) {
	videoID = strings.TrimSpace(videoID)
	videoPath = strings.TrimSpace(videoPath)
	if videoID == "" || videoPath == "" {
		return videoPath, thumbnailPath, nil
	}
	dir := filepath.Dir(videoPath)
	files, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		return videoPath, thumbnailPath, nil
	}

	// VIDEO: rename the preferred (validated, readable) video to <uuid>.video.<ext>,
	// then normalize it to a streamable MP4.
	preferredVideo := pickPreferredVideoPath(ctx, files)
	if preferredVideo != "" {
		ext := strings.ToLower(filepath.Ext(preferredVideo))
		desired := filepath.Join(dir, videoID+".video"+ext)
		if filepath.Clean(preferredVideo) != filepath.Clean(desired) {
			if err := moveOrCopyFile(preferredVideo, desired); err != nil {
				return videoPath, thumbnailPath, err
			}
		}
		videoPath = desired

		// Prune other video containers ONLY when the kept video is a readable,
		// valid video. Never delete a container in favour of an unreadable one —
		// that was the cause of a real archive-deletion bug (a broken .mp4 stub
		// displacing the real .mkv).
		if isReadableVideoFile(ctx, videoPath) {
			for _, p := range files {
				if !isVideoExt(strings.ToLower(filepath.Ext(p))) {
					continue
				}
				if filepath.Clean(p) == filepath.Clean(videoPath) {
					continue
				}
				// Never prune derived assets — only source containers. The hover
				// preview (<uuid>.preview.mp4) is an .mp4 but not a source video.
				base := strings.ToLower(filepath.Base(p))
				if base == "preview.mp4" || strings.Contains(base, ".preview.") {
					continue
				}
				if err := os.Remove(p); err == nil {
					slog.Info("pruned redundant video container", "video_id", videoID, "path", p)
				}
			}
		} else {
			slog.Warn("skip pruning extra video containers: preferred video is not readable",
				"video_id", videoID, "preferred", videoPath)
		}

		// Ensure the canonical video is a browser-playable, faststart MP4.
		if normalized, err := ensureStreamableMP4(ctx, videoPath); err != nil {
			slog.Warn("migrate: normalize to streamable mp4 failed (keeping original)", "path", videoPath, "error", err)
		} else {
			videoPath = normalized
		}
	}

	// INFO: rename any *.info.json to <uuid>.info.json
	for _, p := range files {
		lower := strings.ToLower(filepath.Base(p))
		if strings.HasSuffix(lower, ".info.json") {
			desired := filepath.Join(dir, videoID+".info.json")
			if filepath.Clean(p) != filepath.Clean(desired) {
				_ = moveOrCopyFile(p, desired)
			}
			break
		}
	}

	// CAPTIONS: rename *.vtt to <uuid>.captions.<lang>.vtt
	for _, p := range files {
		lower := strings.ToLower(filepath.Base(p))
		if !strings.HasSuffix(lower, ".vtt") {
			continue
		}
		if strings.HasPrefix(lower, strings.ToLower(videoID)+".captions.") {
			continue
		}
		lang := "und"
		parts := strings.Split(lower, ".")
		if len(parts) >= 2 {
			cand := parts[len(parts)-2]
			if cand != "" && cand != "vtt" {
				lang = cand
			}
		}
		desired := filepath.Join(dir, videoID+".captions."+lang+".vtt")
		if _, err := os.Stat(desired); err == nil {
			continue
		}
		_ = moveOrCopyFile(p, desired)
	}

	// SRC THUMBNAIL: keep the largest image as <uuid>.src_thumbnail.<ext>
	preferredImage := pickPreferredImagePath(files)
	if preferredImage != "" {
		ext := strings.ToLower(filepath.Ext(preferredImage))
		desired := filepath.Join(dir, videoID+".src_thumbnail"+ext)
		if filepath.Clean(preferredImage) != filepath.Clean(desired) {
			_ = moveOrCopyFile(preferredImage, desired)
		}
		for _, p := range files {
			e := strings.ToLower(filepath.Ext(p))
			isImage := e == ".jpg" || e == ".jpeg" || e == ".png" || e == ".webp"
			if !isImage {
				continue
			}
			base := strings.ToLower(filepath.Base(p))
			if strings.HasPrefix(base, strings.ToLower(videoID)+".thumbnail") {
				continue
			}
			if strings.HasPrefix(base, strings.ToLower(videoID)+".src_thumbnail") {
				continue
			}
			// Old ytdlp thumbnail filenames
			if filepath.Clean(p) != filepath.Clean(desired) {
				_ = os.Remove(p)
			}
		}
	}

	// PREVIEW: rename legacy preview.mp4 to <uuid>.preview.mp4
	oldPreview := filepath.Join(dir, "preview.mp4")
	newPreview := filepath.Join(dir, videoID+".preview.mp4")
	if _, err := os.Stat(oldPreview); err == nil {
		if _, err2 := os.Stat(newPreview); err2 != nil {
			_ = moveOrCopyFile(oldPreview, newPreview)
		}
	}

	// THUMBNAIL: rename legacy thumbnail.jpg (or DB thumb path) to <uuid>.thumbnail.jpg
	newThumb := filepath.Join(dir, videoID+".thumbnail.jpg")
	if _, err := os.Stat(newThumb); err == nil {
		thumbnailPath = &newThumb
		return videoPath, thumbnailPath, nil
	}
	if thumbnailPath != nil {
		p := strings.TrimSpace(*thumbnailPath)
		if p != "" {
			if _, err := os.Stat(p); err == nil {
				if filepath.Clean(p) != filepath.Clean(newThumb) {
					_ = moveOrCopyFile(p, newThumb)
				}
				thumbnailPath = &newThumb
				return videoPath, thumbnailPath, nil
			}
		}
	}
	oldThumb := filepath.Join(dir, "thumbnail.jpg")
	if _, err := os.Stat(oldThumb); err == nil {
		_ = moveOrCopyFile(oldThumb, newThumb)
		thumbnailPath = &newThumb
	}

	return videoPath, thumbnailPath, nil
}

func isCanonicalVideoDir(dir string, videoID string) bool {
	dir = filepath.Clean(strings.TrimSpace(dir))
	videoID = strings.TrimSpace(videoID)
	if dir == "" || videoID == "" {
		return false
	}
	if filepath.Base(dir) != videoID {
		return false
	}
	parent := strings.ToLower(strings.TrimSpace(filepath.Base(filepath.Dir(dir))))
	return parent == "downloads" || parent == "download"
}

// migrateVideoAssetsToCanonicalDir moves assets from whatever directory videos.video_path points at
// into the canonical /downloads/<uuid>/ directory, then renames into uuid.<kind>.*.
func migrateVideoAssetsToCanonicalDir(ctx context.Context, videoID string, videoPath string, thumbnailPath *string) (string, *string, error) {
	videoID = strings.TrimSpace(videoID)
	videoPath = strings.TrimSpace(videoPath)
	if videoID == "" || videoPath == "" {
		return videoPath, thumbnailPath, nil
	}

	oldDir := filepath.Dir(videoPath)
	if isCanonicalVideoDir(oldDir, videoID) {
		return migrateVideoDirAssets(ctx, videoID, videoPath, thumbnailPath)
	}

	newDir := filepath.Join(string(filepath.Separator)+"downloads", videoID)
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		return videoPath, thumbnailPath, err
	}

	// Move all files in the old directory into the canonical directory.
	entries, err := filepath.Glob(filepath.Join(oldDir, "*"))
	if err == nil {
		for _, src := range entries {
			base := filepath.Base(src)
			dst := filepath.Join(newDir, base)
			if filepath.Clean(src) == filepath.Clean(dst) {
				continue
			}
			if _, err := os.Stat(dst); err == nil {
				// Destination already exists; drop the source to avoid duplicates.
				_ = os.Remove(src)
				continue
			}
			_ = moveOrCopyFile(src, dst)
		}
	}

	// Best-effort: remove old dir if empty.
	if leftovers, _ := filepath.Glob(filepath.Join(oldDir, "*")); len(leftovers) == 0 {
		_ = os.Remove(oldDir)
	}

	// Provide a "videoPath" inside the newDir so migrateVideoDirAssets scans the right directory.
	stub := filepath.Join(newDir, filepath.Base(videoPath))
	return migrateVideoDirAssets(ctx, videoID, stub, thumbnailPath)
}

func suffixFromFirstDot(filename string) string {
	// Convert youtube_abc123.en.vtt -> .en.vtt (so we can rename to <uuid>.en.vtt)
	// Convert youtube_abc123.info.json -> .info.json
	idx := strings.IndexByte(filename, '.')
	if idx < 0 {
		return ""
	}
	return filename[idx:]
}

// minValidVideoBytes is the smallest size we consider a plausibly-valid video
// container. Anything smaller is treated as a broken/truncated stub and ignored
// by preferred-video selection so it can never displace a real archive file.
const minValidVideoBytes = 1024

// pickPreferredVideoPath chooses the canonical video from a set of files. It
// prefers MP4 (browser-native) then other containers, and within a priority the
// largest file. Candidates are validated with ffprobe: a readable video always
// beats an unreadable/broken one regardless of extension, so a corrupt ".mp4"
// stub can never outrank a real ".mkv" (the cause of a real archive-deletion bug).
// Only if NO candidate is readable does it fall back to the size/extension pick,
// so a transiently-unprobeable file is still returned rather than dropped.
func pickPreferredVideoPath(ctx context.Context, paths []string) string {
	priorities := map[string]int{
		".mp4":  0,
		".webm": 1,
		".mkv":  2,
		".mov":  3,
		".avi":  4,
		".flv":  5,
		".wmv":  5,
		".mpg":  5,
		".mpeg": 5,
		".m4v":  1,
		".ts":   5,
		".mts":  5,
		".m2ts": 5,
		".vob":  5,
		".3gp":  5,
		".ogv":  5,
		".divx": 5,
		".asf":  5,
		".f4v":  5,
		".rm":   6,
		".rmvb": 6,
	}

	pick := func(requireReadable bool) string {
		bestPath := ""
		bestPri := int(^uint(0) >> 1) // max int
		var bestSize int64 = -1

		for _, p := range paths {
			base := strings.ToLower(filepath.Base(p))
			// Never treat previews as the canonical video.
			if base == "preview.mp4" || strings.Contains(base, ".preview.") || strings.HasSuffix(base, ".preview.mp4") {
				continue
			}

			ext := strings.ToLower(filepath.Ext(p))
			pri, ok := priorities[ext]
			if !ok {
				continue
			}

			sz := int64(-1)
			if fi, err := os.Stat(p); err == nil {
				sz = fi.Size()
			}

			// Skip obviously-broken/truncated outputs (e.g. a failed conversion
			// that left an empty ftyp+mdat stub).
			if sz >= 0 && sz < minValidVideoBytes {
				continue
			}

			if requireReadable && !isReadableVideoFile(ctx, p) {
				continue
			}

			if bestPath == "" || pri < bestPri || (pri == bestPri && sz > bestSize) {
				bestPath = p
				bestPri = pri
				bestSize = sz
			}
		}

		return bestPath
	}

	if p := pick(true); p != "" {
		return p
	}
	return pick(false)
}

func isVideoExt(ext string) bool {
	switch ext {
	case ".mp4", ".mkv", ".webm", ".avi", ".mov",
		".flv", ".wmv", ".mpg", ".mpeg", ".m4v",
		".ts", ".mts", ".m2ts", ".vob", ".3gp",
		".ogv", ".divx", ".asf", ".f4v", ".rm", ".rmvb":
		return true
	}
	return false
}

// computeFileHashAndSize computes SHA256 hash and file size
func computeFileHashAndSize(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", 0, err
	}

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", 0, err
	}

	return hex.EncodeToString(h.Sum(nil)), info.Size(), nil
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return err
	}

	// Preserve permissions
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.Chmod(dst, srcInfo.Mode())
}

// isReadableVideoFile reports whether ffprobe can read the file and finds at
// least one video stream. Used to validate candidates before treating one as
// the canonical video (and before deleting any "redundant" sibling), so a
// broken/empty stub can never displace a real archive file.
func isReadableVideoFile(ctx context.Context, path string) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	probe, err := ffmpeg.Probe(probeCtx, path)
	if err != nil {
		return false
	}
	return probe.VideoStreams >= 1 && strings.TrimSpace(probe.VideoCodec) != ""
}

// verifyPlayableMP4 reports whether path is a non-trivial MP4 with a decodable
// video stream and browser-playable codecs. This is the gate that must pass
// before a normalized output is allowed to replace (and trigger deletion of)
// its source.
func verifyPlayableMP4(ctx context.Context, path string) bool {
	fi, err := os.Stat(path)
	if err != nil || fi.Size() < minValidVideoBytes {
		return false
	}
	probeCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	probe, err := ffmpeg.Probe(probeCtx, path)
	if err != nil {
		return false
	}
	if probe.VideoStreams < 1 || strings.TrimSpace(probe.VideoCodec) == "" {
		return false
	}
	// Normalization should have produced browser-playable codecs.
	return !ffmpeg.NeedsVideoTranscode(probe) && !ffmpeg.NeedsAudioTranscode(probe)
}

// ensureStreamableMP4 makes videoPath a browser-playable, faststart MP4 in place.
//
//   - An already-streamable .mp4 just gets faststart applied and is returned as-is.
//   - Otherwise it is normalized in one pass to <base>.mp4 (video copied unless the
//     codec is legacy/unplayable, audio transcoded to AAC only when unplayable,
//     subtitles dropped). The output is ffprobe-verified BEFORE the now-redundant
//     source is deleted, so a failed conversion can never destroy the original.
//
// Returns the path to the streamable video (the new .mp4 on success, the original
// on any failure).
func ensureStreamableMP4(ctx context.Context, videoPath string) (string, error) {
	if strings.TrimSpace(videoPath) == "" {
		return videoPath, nil
	}
	probe, err := ffmpeg.Probe(ctx, videoPath)
	if err != nil {
		return videoPath, fmt.Errorf("ensure streamable: probe %s: %w", videoPath, err)
	}

	ext := strings.ToLower(filepath.Ext(videoPath))
	if ext == ".mp4" && ffmpeg.IsStreamableMP4(probe) {
		if !mp4HasFaststart(videoPath) {
			if err := ffmpeg.ApplyFaststart(ctx, videoPath); err != nil {
				slog.Warn("faststart failed (video still usable)", "path", videoPath, "error", err)
			} else {
				slog.Info("applied faststart to streamable mp4", "path", videoPath)
			}
		}
		return videoPath, nil
	}

	mp4Path := strings.TrimSuffix(videoPath, filepath.Ext(videoPath)) + ".mp4"
	tmpPath := mp4Path + ".normalize.tmp.mp4"
	_ = os.Remove(tmpPath)

	slog.Info("normalizing to streamable mp4",
		"src", videoPath, "video_codec", probe.VideoCodec, "audio_codec", probe.AudioCodec,
		"reencode_video", ffmpeg.NeedsVideoTranscode(probe), "transcode_audio", ffmpeg.NeedsAudioTranscode(probe))

	if err := ffmpeg.NormalizeToStreamableMP4(ctx, videoPath, tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return videoPath, fmt.Errorf("ensure streamable: normalize %s: %w", videoPath, err)
	}
	if !verifyPlayableMP4(ctx, tmpPath) {
		_ = os.Remove(tmpPath)
		return videoPath, fmt.Errorf("ensure streamable: normalized output failed verification: %s", tmpPath)
	}

	// Output verified. Now it is safe to retire the (verified-redundant) source.
	if filepath.Clean(videoPath) != filepath.Clean(mp4Path) {
		if err := os.Remove(videoPath); err != nil {
			slog.Warn("failed to remove source after normalize", "path", videoPath, "error", err)
		}
	}
	if err := os.Rename(tmpPath, mp4Path); err != nil {
		return videoPath, fmt.Errorf("ensure streamable: rename %s -> %s: %w", tmpPath, mp4Path, err)
	}
	slog.Info("normalized to streamable mp4", "path", mp4Path)
	return mp4Path, nil
}
