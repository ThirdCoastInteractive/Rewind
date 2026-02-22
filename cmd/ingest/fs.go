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

	"thirdcoast.systems/rewind/pkg/ffmpeg"
)

// moveVideoToPermanentStorage moves video files from spool to /downloads/{videoID}/
// Returns: videoPath, thumbnailPath, fileHash, fileSize, error
func moveVideoToPermanentStorage(videoID, spoolDir string) (*string, *string, *string, *int64, error) {
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

	preferredVideoSrcPath := pickPreferredVideoPath(files)
	preferredImageSrcPath := pickPreferredImagePath(files)

	var videoPath *string
	var thumbnailPath *string

	// Move each file to permanent storage
	for _, srcPath := range files {
		filename := filepath.Base(srcPath)
		ext := strings.ToLower(filepath.Ext(filename))

		isVideo := ext == ".mp4" || ext == ".mkv" || ext == ".webm" || ext == ".avi" || ext == ".mov"
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
	if ext == ".mp4" || ext == ".mkv" || ext == ".webm" || ext == ".avi" || ext == ".mov" {
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

func migrateVideoDirAssets(videoID string, videoPath string, thumbnailPath *string) (string, *string, error) {
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

	// VIDEO: rename preferred to <uuid>.video.<ext> and delete other video containers.
	preferredVideo := pickPreferredVideoPath(files)
	if preferredVideo != "" {
		ext := strings.ToLower(filepath.Ext(preferredVideo))
		desired := filepath.Join(dir, videoID+".video"+ext)
		if filepath.Clean(preferredVideo) != filepath.Clean(desired) {
			if err := moveOrCopyFile(preferredVideo, desired); err != nil {
				return videoPath, thumbnailPath, err
			}
		}
		videoPath = desired
		for _, p := range files {
			e := strings.ToLower(filepath.Ext(p))
			isVideo := e == ".mp4" || e == ".mkv" || e == ".webm" || e == ".avi" || e == ".mov"
			if !isVideo {
				continue
			}
			if filepath.Clean(p) == filepath.Clean(videoPath) {
				continue
			}
			_ = os.Remove(p)
		}

		// Migrate non-MP4 files to MP4 for browser compatibility
		videoPath = migrateVideoToMp4(videoPath)
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
func migrateVideoAssetsToCanonicalDir(videoID string, videoPath string, thumbnailPath *string) (string, *string, error) {
	videoID = strings.TrimSpace(videoID)
	videoPath = strings.TrimSpace(videoPath)
	if videoID == "" || videoPath == "" {
		return videoPath, thumbnailPath, nil
	}

	oldDir := filepath.Dir(videoPath)
	if isCanonicalVideoDir(oldDir, videoID) {
		return migrateVideoDirAssets(videoID, videoPath, thumbnailPath)
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
	return migrateVideoDirAssets(videoID, stub, thumbnailPath)
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

func pickPreferredVideoPath(paths []string) string {
	// Prefer MP4 when available (we remux to mp4 for browser compatibility), otherwise fall back.
	// Within the same extension priority, prefer the largest file.
	priorities := map[string]int{
		".mp4":  0,
		".webm": 1,
		".mkv":  2,
		".mov":  3,
		".avi":  4,
	}

	bestPath := ""
	bestPri := int(^uint(0) >> 1) // max int
	var bestSize int64 = -1

	for _, p := range paths {
		base := strings.ToLower(filepath.Base(p))
		// Never treat previews as the canonical video.
		// Old layout: preview.mp4
		// New layout: <uuid>.preview.mp4
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

		if bestPath == "" || pri < bestPri || (pri == bestPri && sz > bestSize) {
			bestPath = p
			bestPri = pri
			bestSize = sz
		}
	}

	return bestPath
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

// remuxToMp4 remuxes a video file to MP4 format using ffmpeg.
// This is a migration step for existing videos that were remuxed to MKV or other formats.
// Adds Rewind branding metadata to the output.
// Returns the new mp4 path if successful, or empty string if remux failed/skipped.
func remuxToMp4(srcPath string) (string, error) {
	ext := strings.ToLower(filepath.Ext(srcPath))
	if ext == ".mp4" {
		// Already mp4, just add metadata if missing
		return srcPath, nil
	}
	if ext != ".mkv" && ext != ".webm" && ext != ".avi" && ext != ".mov" {
		return "", nil
	}

	// Check if file exists
	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		return "", nil
	}

	// Build mp4 path
	mp4Path := strings.TrimSuffix(srcPath, filepath.Ext(srcPath)) + ".mp4"

	// If mp4 already exists, skip remux
	if _, err := os.Stat(mp4Path); err == nil {
		slog.Debug("mp4 already exists, skipping remux", "src", srcPath, "mp4", mp4Path)
		return mp4Path, nil
	}

	slog.Info("remuxing to mp4", "src", srcPath, "dest", mp4Path)

	// Use ffmpeg to remux (copy streams, no re-encoding)
	// Add Rewind branding metadata and strip source handler names
	if err := ffmpeg.Remux(context.Background(), srcPath, mp4Path, &ffmpeg.RemuxOptions{
		Metadata: map[string]string{
			"encoded_by":         "Rewind Video Archive",
			"comment":            "Archived with Rewind",
			"handler_name":       "Rewind Video Archive Handler",
			"s:v:0:handler_name": "Rewind Video Archive Handler",
			"s:a:0:handler_name": "Rewind Video Archive Handler",
		},
	}); err != nil {
		slog.Error("ffmpeg remux failed", "error", err, "src", srcPath)
		// Clean up partial output
		_ = os.Remove(mp4Path)
		return "", fmt.Errorf("ffmpeg remux failed: %w", err)
	}

	// Verify mp4 was created
	if _, err := os.Stat(mp4Path); os.IsNotExist(err) {
		return "", fmt.Errorf("mp4 file not created after remux")
	}

	// Remove the original file
	if err := os.Remove(srcPath); err != nil {
		slog.Warn("failed to remove original after remux", "path", srcPath, "error", err)
	}

	slog.Info("remux complete", "mp4", mp4Path)
	return mp4Path, nil
}

// migrateVideoToMp4 checks if a video is in a non-MP4 format and remuxes it to MP4.
// This is called during ingest to migrate existing videos.
// Returns the updated video path (mp4 if migrated, original otherwise).
func migrateVideoToMp4(videoPath string) string {
	if videoPath == "" {
		return videoPath
	}

	ext := strings.ToLower(filepath.Ext(videoPath))
	if ext == ".mp4" {
		return videoPath
	}
	if ext != ".mkv" && ext != ".webm" && ext != ".avi" && ext != ".mov" {
		return videoPath
	}

	mp4Path, err := remuxToMp4(videoPath)
	if err != nil {
		slog.Warn("failed to migrate to mp4", "path", videoPath, "error", err)
		return videoPath // Keep original on failure
	}

	if mp4Path != "" {
		return mp4Path
	}

	return videoPath
}
