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

// hlsStreamInfo describes one audio stream extracted from ffprobe data.
type hlsStreamInfo struct {
	Index    int
	Language string
	Channels int
	Codec    string
	Title    string // from tags
	Default  bool
}

// generateHLS takes a muxed video file and produces an HLS master playlist
// with separate video and audio playlists.
// It demuxes each stream, fragments into fMP4 segments, and writes master.m3u8.
// Returns the path to master.m3u8 or empty string if generation was skipped.
func generateHLS(ctx context.Context, videoPath, videoID string) (string, error) {
	hlsDir := filepath.Join(filepath.Dir(videoPath), "hls")

	// If master.m3u8 already exists, skip
	masterPath := filepath.Join(hlsDir, "master.m3u8")
	if _, err := os.Stat(masterPath); err == nil {
		slog.Info("HLS already exists, skipping", "video_id", videoID)
		return masterPath, nil
	}

	// Probe to discover streams
	probeResult, err := ffmpeg.Probe(ctx, videoPath)
	if err != nil {
		return "", fmt.Errorf("hls probe: %w", err)
	}

	// Extract stream details from raw JSON
	rawStreams, audioInfos := parseStreamsForHLS(probeResult.RawJSON)
	if len(rawStreams) == 0 {
		return "", fmt.Errorf("hls: no streams found in %s", videoPath)
	}

	// We need at least a video stream
	hasVideo := false
	for _, s := range rawStreams {
		if s.codecType == "video" {
			hasVideo = true
			break
		}
	}
	if !hasVideo {
		slog.Warn("HLS generation skipped: no video stream", "video_id", videoID)
		return "", nil
	}

	if err := os.MkdirAll(hlsDir, 0o755); err != nil {
		return "", fmt.Errorf("hls: mkdir: %w", err)
	}

	// Temp dir for demuxed intermediates
	tmpDir := filepath.Join(hlsDir, ".tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return "", fmt.Errorf("hls: mkdir tmp: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// 1. Demux and generate HLS for the video track
	slog.Info("HLS: demuxing video track", "video_id", videoID)
	videoOnly := filepath.Join(tmpDir, "video_only.mp4")
	if err := ffmpeg.DemuxStream(ctx, videoPath, videoOnly, "0:v:0"); err != nil {
		return "", fmt.Errorf("hls: demux video: %w", err)
	}

	slog.Info("HLS: generating video segments", "video_id", videoID)
	if err := ffmpeg.GenerateHLSPlaylist(ctx, videoOnly, hlsDir, "video", 6); err != nil {
		return "", fmt.Errorf("hls: generate video playlist: %w", err)
	}

	// 2. Demux and generate HLS for each audio track
	var hlsAudioTracks []ffmpeg.HLSTrack
	for i, info := range audioInfos {
		slog.Info("HLS: demuxing audio track", "video_id", videoID, "index", i, "lang", info.Language, "channels", info.Channels)

		audioFile := filepath.Join(tmpDir, fmt.Sprintf("audio_%d.m4a", i))
		mapSpec := fmt.Sprintf("0:a:%d", i)
		if err := ffmpeg.DemuxStream(ctx, videoPath, audioFile, mapSpec); err != nil {
			slog.Warn("HLS: failed to demux audio track, skipping", "video_id", videoID, "index", i, "error", err)
			continue
		}

		prefix := fmt.Sprintf("audio_%d", i)
		slog.Info("HLS: generating audio segments", "video_id", videoID, "index", i)
		if err := ffmpeg.GenerateHLSPlaylist(ctx, audioFile, hlsDir, prefix, 6); err != nil {
			slog.Warn("HLS: failed to generate audio playlist, skipping", "video_id", videoID, "index", i, "error", err)
			continue
		}

		name := buildAudioTrackName(info)
		hlsAudioTracks = append(hlsAudioTracks, ffmpeg.HLSTrack{
			Type:         "audio",
			Index:        i,
			Name:         name,
			Language:     info.Language,
			Channels:     info.Channels,
			Default:      i == 0, // first audio track is default
			PlaylistFile: prefix + ".m3u8",
		})
	}

	// 3. Write master playlist
	slog.Info("HLS: writing master playlist", "video_id", videoID, "audio_tracks", len(hlsAudioTracks))

	videoBandwidth := 0
	if probeResult.Bitrate > 0 {
		videoBandwidth = int(probeResult.Bitrate)
	} else {
		// Estimate from file size and duration
		if probeResult.Duration > 0 && probeResult.Size > 0 {
			videoBandwidth = int(float64(probeResult.Size*8) / probeResult.Duration)
		} else {
			videoBandwidth = 10_000_000 // 10 Mbps fallback
		}
	}

	if err := ffmpeg.WriteMasterPlaylist(masterPath, hlsAudioTracks, "video.m3u8", videoBandwidth, ""); err != nil {
		return "", fmt.Errorf("hls: write master playlist: %w", err)
	}

	slog.Info("HLS generation complete", "video_id", videoID, "master", masterPath, "audio_tracks", len(hlsAudioTracks))
	return masterPath, nil
}

// regenerateHLS forces regeneration by removing existing HLS dir first.
// It produces a multi-variant master playlist that includes the main video
// plus any additional quality files stored in the streams/ subdirectory.
func regenerateHLS(ctx context.Context, videoPath, videoID string) (string, error) {
	hlsDir := filepath.Join(filepath.Dir(videoPath), "hls")
	if err := os.RemoveAll(hlsDir); err != nil && !os.IsNotExist(err) {
		slog.Warn("failed to remove existing HLS directory", "path", hlsDir, "error", err)
	}

	permanentDir := filepath.Dir(videoPath)
	streamsDir := filepath.Join(permanentDir, "streams")

	// Collect all stream video files
	var streamFiles []string
	if entries, err := os.ReadDir(streamsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if ext == ".mp4" || ext == ".mkv" || ext == ".webm" || ext == ".mov" {
				streamFiles = append(streamFiles, filepath.Join(streamsDir, e.Name()))
			}
		}
	}

	// If no extra streams, just generate from main video as before
	if len(streamFiles) == 0 {
		return generateHLS(ctx, videoPath, videoID)
	}

	return generateMultiVariantHLS(ctx, videoPath, streamFiles, videoID)
}

// generateMultiVariantHLS creates an HLS master playlist with multiple video
// quality variants: the main video plus each file in streams/.
func generateMultiVariantHLS(ctx context.Context, mainVideoPath string, streamFiles []string, videoID string) (string, error) {
	hlsDir := filepath.Join(filepath.Dir(mainVideoPath), "hls")
	if err := os.MkdirAll(hlsDir, 0o755); err != nil {
		return "", fmt.Errorf("multi-hls: mkdir: %w", err)
	}

	masterPath := filepath.Join(hlsDir, "master.m3u8")

	// Probe to discover audio tracks from the main video
	mainProbe, err := ffmpeg.Probe(ctx, mainVideoPath)
	if err != nil {
		return "", fmt.Errorf("multi-hls: probe main: %w", err)
	}

	rawStreams, audioInfos := parseStreamsForHLS(mainProbe.RawJSON)
	if len(rawStreams) == 0 {
		return "", fmt.Errorf("multi-hls: no streams found in %s", mainVideoPath)
	}

	tmpDir := filepath.Join(hlsDir, ".tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return "", fmt.Errorf("multi-hls: mkdir tmp: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// === Audio tracks (from main video only) ===
	var hlsAudioTracks []ffmpeg.HLSTrack
	for i, info := range audioInfos {
		slog.Info("multi-HLS: demuxing audio track", "video_id", videoID, "index", i)
		audioFile := filepath.Join(tmpDir, fmt.Sprintf("audio_%d.m4a", i))
		mapSpec := fmt.Sprintf("0:a:%d", i)
		if err := ffmpeg.DemuxStream(ctx, mainVideoPath, audioFile, mapSpec); err != nil {
			slog.Warn("multi-HLS: failed to demux audio track", "index", i, "error", err)
			continue
		}

		prefix := fmt.Sprintf("audio_%d", i)
		if err := ffmpeg.GenerateHLSPlaylist(ctx, audioFile, hlsDir, prefix, 6); err != nil {
			slog.Warn("multi-HLS: failed to generate audio playlist", "index", i, "error", err)
			continue
		}

		hlsAudioTracks = append(hlsAudioTracks, ffmpeg.HLSTrack{
			Type:         "audio",
			Index:        i,
			Name:         buildAudioTrackName(info),
			Language:     info.Language,
			Channels:     info.Channels,
			Default:      i == 0,
			PlaylistFile: prefix + ".m3u8",
		})
	}

	// === Video variants ===
	var variants []ffmpeg.VideoVariant

	// Helper to add a video variant from a source file
	addVariant := func(srcPath, prefix string) {
		probe, err := ffmpeg.Probe(ctx, srcPath)
		if err != nil {
			slog.Warn("multi-HLS: failed to probe", "path", srcPath, "error", err)
			return
		}

		// Demux video track
		videoOnly := filepath.Join(tmpDir, prefix+"_video_only.mp4")
		if err := ffmpeg.DemuxStream(ctx, srcPath, videoOnly, "0:v:0"); err != nil {
			slog.Warn("multi-HLS: failed to demux video", "path", srcPath, "error", err)
			return
		}

		playlistPrefix := prefix + "_video"
		if err := ffmpeg.GenerateHLSPlaylist(ctx, videoOnly, hlsDir, playlistPrefix, 6); err != nil {
			slog.Warn("multi-HLS: failed to generate video playlist", "path", srcPath, "error", err)
			return
		}

		bandwidth := 0
		if probe.Bitrate > 0 {
			bandwidth = int(probe.Bitrate)
		} else if probe.Duration > 0 && probe.Size > 0 {
			bandwidth = int(float64(probe.Size*8) / probe.Duration)
		} else {
			bandwidth = 2_000_000 // 2 Mbps fallback
		}

		variants = append(variants, ffmpeg.VideoVariant{
			Width:        probe.Width,
			Height:       probe.Height,
			Bandwidth:    bandwidth,
			PlaylistFile: playlistPrefix + ".m3u8",
		})

		slog.Info("multi-HLS: added variant",
			"path", srcPath, "prefix", prefix,
			"resolution", fmt.Sprintf("%dx%d", probe.Width, probe.Height),
			"bandwidth", bandwidth)
	}

	// Main video = primary variant
	addVariant(mainVideoPath, "main")

	// Additional stream files
	for i, sf := range streamFiles {
		prefix := fmt.Sprintf("stream_%d", i)
		addVariant(sf, prefix)
	}

	if len(variants) == 0 {
		return "", fmt.Errorf("multi-hls: no video variants produced")
	}

	// Write multi-variant master playlist
	if err := ffmpeg.WriteMasterPlaylistMulti(masterPath, hlsAudioTracks, variants); err != nil {
		return "", fmt.Errorf("multi-hls: write master: %w", err)
	}

	slog.Info("multi-variant HLS generation complete",
		"video_id", videoID,
		"variants", len(variants),
		"audio_tracks", len(hlsAudioTracks))
	return masterPath, nil
}

// probeStream holds basic info parsed from the raw ffprobe JSON.
type probeStream struct {
	index     int
	codecType string
	codecName string
}

// parseStreamsForHLS extracts stream info from ffprobe RawJSON.
func parseStreamsForHLS(rawJSON map[string]any) ([]probeStream, []hlsStreamInfo) {
	var streams []probeStream
	var audioInfos []hlsStreamInfo

	rawStreams, ok := rawJSON["streams"].([]any)
	if !ok {
		return nil, nil
	}

	audioIndex := 0
	for _, rawS := range rawStreams {
		s, ok := rawS.(map[string]any)
		if !ok {
			continue
		}
		codecType, _ := s["codec_type"].(string)
		codecName, _ := s["codec_name"].(string)
		index := 0
		if v, ok := s["index"].(float64); ok {
			index = int(v)
		}

		streams = append(streams, probeStream{
			index:     index,
			codecType: codecType,
			codecName: codecName,
		})

		if codecType == "audio" {
			info := hlsStreamInfo{
				Index: audioIndex,
				Codec: codecName,
			}

			if ch, ok := s["channels"].(float64); ok {
				info.Channels = int(ch)
			}

			// Extract language from tags
			if tags, ok := s["tags"].(map[string]any); ok {
				if lang, ok := tags["language"].(string); ok && lang != "und" {
					info.Language = lang
				}
				if title, ok := tags["title"].(string); ok {
					info.Title = title
				}
			}

			audioInfos = append(audioInfos, info)
			audioIndex++
		}
	}

	return streams, audioInfos
}

// buildAudioTrackName constructs a human-readable name for an audio track.
func buildAudioTrackName(info hlsStreamInfo) string {
	parts := []string{}

	if info.Title != "" {
		parts = append(parts, info.Title)
	} else if info.Language != "" {
		parts = append(parts, strings.ToUpper(info.Language))
	} else {
		parts = append(parts, fmt.Sprintf("Track %d", info.Index+1))
	}

	if info.Channels > 0 {
		switch info.Channels {
		case 1:
			parts = append(parts, "(Mono)")
		case 2:
			parts = append(parts, "(Stereo)")
		case 6:
			parts = append(parts, "(5.1)")
		case 8:
			parts = append(parts, "(7.1)")
		default:
			parts = append(parts, fmt.Sprintf("(%dch)", info.Channels))
		}
	}

	return strings.Join(parts, " ")
}

// isFormatSpecificDownload checks if a download used -f to request a specific format.
func isFormatSpecificDownload(extraArgs []string) bool {
	for _, arg := range extraArgs {
		if arg == "-f" {
			return true
		}
	}
	return false
}

// hasHLS checks if HLS has been generated for a video.
func hasHLS(videoPath string) bool {
	hlsDir := filepath.Join(filepath.Dir(videoPath), "hls")
	masterPath := filepath.Join(hlsDir, "master.m3u8")
	_, err := os.Stat(masterPath)
	return err == nil
}

// hlsReadyFromAssetsStatus checks the HLS status from the video's assets_status JSON.
func hlsReadyFromAssetsStatus(status map[string]any) bool {
	if v, ok := status["hls"]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// mergeFormatDownload handles a format-specific download by storing the file alongside
// the main video without overwriting it, then regenerating HLS.
// Returns nil if this was not a format-specific download (caller should handle normally).
func mergeFormatDownload(ctx context.Context, videoID, spoolDir, videoPath string) error {
	// Find video files in spool
	spoolFiles, err := filepath.Glob(filepath.Join(spoolDir, "*"))
	if err != nil {
		return fmt.Errorf("merge format: glob spool: %w", err)
	}

	permanentDir := filepath.Dir(videoPath)

	// Move files from spool to permanent dir, but DON'T overwrite the main video.
	// Rename new files with a "format_" prefix to distinguish from the main file.
	for _, srcPath := range spoolFiles {
		filename := filepath.Base(srcPath)
		ext := strings.ToLower(filepath.Ext(filename))

		// Skip info.json — we already have it from the original download
		if strings.HasSuffix(strings.ToLower(filename), ".info.json") {
			os.Remove(srcPath)
			continue
		}

		// Store format-specific video files in a streams/ subdirectory
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
			// Avoid overwriting existing streams — deduplicate with counter
			if _, err := os.Stat(destPath); err == nil {
				base := strings.TrimSuffix(filename, ext) // "twitter_12345_NA"
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
			} else {
				slog.Info("merge format: stored stream file", "dest", destPath)

				// Post-process: mux the first audio track from the main
				// video into the stream file (yt-dlp format downloads are
				// often video-only) and apply faststart for instant seeking.
				muxedPath := destPath + ".muxing.mp4"
				if err := ffmpeg.MuxVideoAudio(ctx, destPath, videoPath, muxedPath); err != nil {
					slog.Warn("merge format: audio mux failed (keeping video-only)",
						"dest", destPath, "error", err)
					os.Remove(muxedPath) // clean up partial output
				} else {
					// Replace original with muxed version
					_ = os.Remove(destPath)
					if err := os.Rename(muxedPath, destPath); err != nil {
						slog.Warn("merge format: rename muxed file failed",
							"src", muxedPath, "dest", destPath, "error", err)
					} else {
						slog.Info("merge format: muxed audio + faststart",
							"dest", destPath)
					}
				}
			}
			continue
		}

		// For other files (thumbnails, etc), store normally
		destPath := filepath.Join(permanentDir, filename)
		if err := moveOrCopyFile(srcPath, destPath); err != nil {
			slog.Warn("merge format: failed to move file", "src", srcPath, "dest", destPath, "error", err)
		}
	}

	// Clean up spool
	_ = os.RemoveAll(spoolDir)

	// Regenerate HLS from the main video + streams.
	if _, err := regenerateHLS(ctx, videoPath, videoID); err != nil {
		slog.Warn("merge format: HLS generation failed", "video_id", videoID, "error", err)
		// Non-fatal — the format file is stored, HLS can be retried
	}

	// Write streams manifest so the web UI knows which heights are downloaded.
	writeStreamsManifest(ctx, videoPath)

	return nil
}

// probeDataJSON probes a video file and returns the result as JSON bytes for DB storage.
func probeDataJSON(ctx context.Context, videoPath string) ([]byte, *ffmpeg.ProbeResult) {
	probeResult, err := ffmpeg.Probe(ctx, videoPath)
	if err != nil {
		slog.Warn("failed to probe video", "path", videoPath, "error", err)
		return nil, nil
	}
	pj, err := json.Marshal(probeResult.RawJSON)
	if err != nil {
		return nil, probeResult
	}
	return pj, probeResult
}

// StreamsManifest describes additional downloaded stream files.
// Written to streams/manifest.json by the ingest service, read by the web UI.
type StreamsManifest struct {
	Streams []StreamEntry `json:"streams"`
}

// StreamEntry describes one downloaded stream file.
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
