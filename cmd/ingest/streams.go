package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"thirdcoast.systems/rewind/pkg/ffmpeg"
)

// isFormatSpecificDownload checks if a download used -f to request a specific format.
// These are supplementary quality downloads (e.g. a 1080p chip) that are stored
// alongside the main video in streams/ rather than replacing it.
func isFormatSpecificDownload(extraArgs []string) bool {
	for _, arg := range extraArgs {
		if arg == "-f" {
			return true
		}
	}
	return false
}

// mergeFormatDownload handles a format-specific download by storing the file alongside
// the main video without overwriting it. The web UI offers these as alternate quality
// sources (direct <source> swaps — no HLS involved).
func mergeFormatDownload(ctx context.Context, videoID, spoolDir, videoPath string) error {
	spoolFiles, err := filepath.Glob(filepath.Join(spoolDir, "*"))
	if err != nil {
		return fmt.Errorf("merge format: glob spool: %w", err)
	}

	permanentDir := filepath.Dir(videoPath)

	for _, srcPath := range spoolFiles {
		filename := filepath.Base(srcPath)
		ext := strings.ToLower(filepath.Ext(filename))

		// Skip info.json — we already have it from the original download.
		if strings.HasSuffix(strings.ToLower(filename), ".info.json") {
			os.Remove(srcPath)
			continue
		}

		// Store format-specific video files in a streams/ subdirectory.
		// Append a counter suffix if a file with the same name already exists,
		// since yt-dlp generates identical filenames regardless of format_id.
		isVideo := ext == ".mp4" || ext == ".mkv" || ext == ".webm" || ext == ".avi" || ext == ".mov"
		if isVideo {
			streamsDir := filepath.Join(permanentDir, "streams")
			if err := os.MkdirAll(streamsDir, 0o755); err != nil {
				slog.Warn("merge format: failed to create streams dir", "error", err)
				continue
			}
			destPath := filepath.Join(streamsDir, filename)
			if _, err := os.Stat(destPath); err == nil {
				base := strings.TrimSuffix(filename, ext)
				for i := 2; i < 100; i++ {
					candidate := filepath.Join(streamsDir, fmt.Sprintf("%s_%d%s", base, i, ext))
					if _, err := os.Stat(candidate); os.IsNotExist(err) {
						destPath = candidate
						break
					}
				}
			}
			if err := moveOrCopyFile(srcPath, destPath); err != nil {
				slog.Warn("merge format: failed to move file", "src", srcPath, "dest", destPath, "error", err)
				continue
			}
			slog.Info("merge format: stored stream file", "dest", destPath)

			// Normalize the stream file to a browser-playable MP4 so it can be
			// used directly as an alternate <source>. yt-dlp format downloads are
			// often video-only or use a non-streamable codec; normalization copies
			// the audio from the main video is NOT needed here because mergeall
			// downloads already include audio. We just ensure it's a faststart MP4.
			normalizeStreamFileInPlace(ctx, destPath)
			continue
		}

		// Other files (thumbnails, etc.) are stored next to the main video.
		destPath := filepath.Join(permanentDir, filename)
		if err := moveOrCopyFile(srcPath, destPath); err != nil {
			slog.Warn("merge format: failed to move file", "src", srcPath, "dest", destPath, "error", err)
		}
	}

	_ = os.RemoveAll(spoolDir)

	// Refresh the streams manifest so the web UI knows which heights are available.
	writeStreamsManifest(ctx, videoPath)
	return nil
}

// normalizeStreamFileInPlace ensures an alternate-quality stream file is a
// browser-playable, faststart MP4. On success it replaces the original file;
// on failure it leaves the original untouched.
func normalizeStreamFileInPlace(ctx context.Context, path string) {
	probe, err := ffmpeg.Probe(ctx, path)
	if err != nil {
		slog.Warn("merge format: probe stream file failed", "path", path, "error", err)
		return
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".mp4" && ffmpeg.IsStreamableMP4(probe) {
		if err := ffmpeg.ApplyFaststart(ctx, path); err != nil {
			slog.Warn("merge format: faststart failed", "path", path, "error", err)
		}
		return
	}

	mp4Path := strings.TrimSuffix(path, filepath.Ext(path)) + ".mp4"
	tmpPath := mp4Path + ".normalize.tmp.mp4"
	if err := ffmpeg.NormalizeToStreamableMP4(ctx, path, tmpPath); err != nil {
		slog.Warn("merge format: normalize stream file failed (keeping original)", "path", path, "error", err)
		_ = os.Remove(tmpPath)
		return
	}
	if !verifyPlayableMP4(ctx, tmpPath) {
		slog.Warn("merge format: normalized stream file failed verification (keeping original)", "path", path)
		_ = os.Remove(tmpPath)
		return
	}
	if path != mp4Path {
		_ = os.Remove(path)
	}
	if err := os.Rename(tmpPath, mp4Path); err != nil {
		slog.Warn("merge format: rename normalized stream file failed", "tmp", tmpPath, "dest", mp4Path, "error", err)
	}
}

// StreamsManifest describes additional downloaded stream files.
// Written to streams/manifest.json by ingest, read by the web UI.
type StreamsManifest struct {
	Streams []StreamEntry `json:"streams"`
}

// StreamEntry describes one downloaded alternate-quality stream file.
type StreamEntry struct {
	Filename string `json:"filename"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Codec    string `json:"codec"`
}

// writeStreamsManifest scans the streams/ directory for video files, probes each,
// and writes a manifest.json with resolution info for the web UI.
func writeStreamsManifest(ctx context.Context, mainVideoPath string) {
	permanentDir := filepath.Dir(mainVideoPath)
	streamsDir := filepath.Join(permanentDir, "streams")

	entries, err := os.ReadDir(streamsDir)
	if err != nil {
		return // no streams dir
	}

	var manifest StreamsManifest
	for _, e := range entries {
		if e.IsDir() || e.Name() == "manifest.json" {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".mp4" && ext != ".mkv" && ext != ".webm" && ext != ".mov" {
			continue
		}
		fullPath := filepath.Join(streamsDir, e.Name())
		probe, err := ffmpeg.Probe(ctx, fullPath)
		if err != nil {
			slog.Warn("streams manifest: failed to probe", "file", e.Name(), "error", err)
			continue
		}
		manifest.Streams = append(manifest.Streams, StreamEntry{
			Filename: e.Name(),
			Width:    probe.Width,
			Height:   probe.Height,
			Codec:    probe.VideoCodec,
		})
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		slog.Warn("streams manifest: failed to marshal", "error", err)
		return
	}

	manifestPath := filepath.Join(streamsDir, "manifest.json")
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		slog.Warn("streams manifest: failed to write", "path", manifestPath, "error", err)
	} else {
		slog.Info("streams manifest: written", "path", manifestPath, "streams", len(manifest.Streams))
	}
}
