package ffmpeg

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// HLSTrack describes one media track to include in the HLS master playlist.
type HLSTrack struct {
	Type     string // "video" or "audio"
	Index    int    // stream index within the source file (e.g. 0:a:2)
	Name     string // human-readable label (e.g. "English (5.1 Surround)")
	Language string // BCP-47 language code (e.g. "en")
	Channels int    // audio channel count (0 for video)
	Codec    string // codec string for EXT-X-STREAM-INF (e.g. "mp4a.40.2")
	Default  bool   // is this the default track?

	// Set after HLS generation
	PlaylistFile string // relative filename (e.g. "audio_0.m3u8")
}

// DemuxStream extracts a single stream from input to output using stream copy (no re-encode).
// streamSpec is an ffmpeg map specifier like "0:v:0" or "0:a:2".
func DemuxStream(ctx context.Context, input, output, streamSpec string) error {
	return Run(ctx, input, output,
		MapStream(streamSpec),
		CopyAll,
	)
}

// GenerateHLSPlaylist fragments a single-stream file into fMP4 HLS segments.
// segmentDuration is the target segment length in seconds (default 6).
// outputDir receives the .m3u8 playlist and .m4s segments.
// prefix is used to name files (e.g. "video" → video.m3u8, video_000.m4s).
func GenerateHLSPlaylist(ctx context.Context, input, outputDir string, prefix string, segmentDuration int) error {
	if segmentDuration <= 0 {
		segmentDuration = 6
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("hls: mkdir %s: %w", outputDir, err)
	}

	playlistPath := filepath.Join(outputDir, prefix+".m3u8")
	segmentPattern := filepath.Join(outputDir, prefix+"_%03d.m4s")

	cmd := NewCommand(input, playlistPath,
		CopyAll,
		ExtraArgs(
			"-movflags", "+frag_keyframe+empty_moov",
			"-hls_time", fmt.Sprintf("%d", segmentDuration),
			"-hls_playlist_type", "vod",
			"-hls_segment_type", "fmp4",
			"-hls_segment_filename", segmentPattern,
		),
	)
	return cmd.Run(ctx)
}

// MasterPlaylistEntry is one line-group in the master playlist.
type MasterPlaylistEntry struct {
	// For audio/subtitle alternate renditions
	Type       string // "AUDIO" or "SUBTITLES" (empty for the main video stream-inf)
	GroupID    string // e.g. "audio"
	Name       string // e.g. "English (5.1 Surround)"
	Language   string // e.g. "en"
	Channels   string // e.g. "6"
	Default    bool
	URI        string // relative path to the media playlist
	Bandwidth  int    // only for EXT-X-STREAM-INF
	Codecs     string // e.g. "av01.0.12M.10,mp4a.40.2"
	AudioGroup string // audio group reference for video stream inf
}

// VideoVariant describes one video quality variant in the HLS master playlist.
type VideoVariant struct {
	Width        int    // video width in pixels
	Height       int    // video height in pixels
	Bandwidth    int    // bits per second
	Codecs       string // optional codec string for EXT-X-STREAM-INF
	PlaylistFile string // relative path from master.m3u8 (e.g. "video.m3u8" or "720p/video.m3u8")
}

// WriteMasterPlaylistMulti writes an HLS master playlist with multiple video variants
// and optional audio tracks. Variants are sorted highest bandwidth first.
func WriteMasterPlaylistMulti(path string, audioTracks []HLSTrack, variants []VideoVariant) error {
	var b strings.Builder
	b.WriteString("#EXTM3U\n\n")

	hasAudio := len(audioTracks) > 0
	audioGroupID := "audio"

	for _, t := range audioTracks {
		chStr := ""
		if t.Channels > 0 {
			chStr = fmt.Sprintf(",CHANNELS=\"%d\"", t.Channels)
		}
		langStr := ""
		if t.Language != "" {
			langStr = fmt.Sprintf(",LANGUAGE=\"%s\"", t.Language)
		}
		defStr := "NO"
		autoStr := "NO"
		if t.Default {
			defStr = "YES"
			autoStr = "YES"
		}

		b.WriteString(fmt.Sprintf(
			"#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"%s\",NAME=\"%s\"%s%s,DEFAULT=%s,AUTOSELECT=%s,URI=\"%s\"\n",
			audioGroupID, t.Name, langStr, chStr, defStr, autoStr, t.PlaylistFile,
		))
	}

	if hasAudio {
		b.WriteString("\n")
	}

	// Sort variants by bandwidth descending (highest quality first)
	sort.Slice(variants, func(i, j int) bool {
		return variants[i].Bandwidth > variants[j].Bandwidth
	})

	for _, v := range variants {
		audioRef := ""
		if hasAudio {
			audioRef = fmt.Sprintf(",AUDIO=\"%s\"", audioGroupID)
		}
		codecsStr := ""
		if v.Codecs != "" {
			codecsStr = fmt.Sprintf(",CODECS=\"%s\"", v.Codecs)
		}
		resStr := ""
		if v.Width > 0 && v.Height > 0 {
			resStr = fmt.Sprintf(",RESOLUTION=%dx%d", v.Width, v.Height)
		}
		b.WriteString(fmt.Sprintf(
			"#EXT-X-STREAM-INF:BANDWIDTH=%d%s%s%s\n%s\n",
			v.Bandwidth, resStr, codecsStr, audioRef, v.PlaylistFile,
		))
	}

	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// WriteMasterPlaylist writes an HLS master playlist referencing the given entries.
func WriteMasterPlaylist(path string, audioTracks []HLSTrack, videoPlaylist string, videoBandwidth int, videoCodecs string) error {
	var b strings.Builder
	b.WriteString("#EXTM3U\n\n")

	hasAudio := len(audioTracks) > 0
	audioGroupID := "audio"

	for _, t := range audioTracks {
		chStr := ""
		if t.Channels > 0 {
			chStr = fmt.Sprintf(",CHANNELS=\"%d\"", t.Channels)
		}
		langStr := ""
		if t.Language != "" {
			langStr = fmt.Sprintf(",LANGUAGE=\"%s\"", t.Language)
		}
		defStr := "NO"
		autoStr := "NO"
		if t.Default {
			defStr = "YES"
			autoStr = "YES"
		}

		b.WriteString(fmt.Sprintf(
			"#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"%s\",NAME=\"%s\"%s%s,DEFAULT=%s,AUTOSELECT=%s,URI=\"%s\"\n",
			audioGroupID, t.Name, langStr, chStr, defStr, autoStr, t.PlaylistFile,
		))
	}

	if hasAudio {
		b.WriteString("\n")
	}

	// Video stream inf
	audioRef := ""
	if hasAudio {
		audioRef = fmt.Sprintf(",AUDIO=\"%s\"", audioGroupID)
	}
	codecsStr := ""
	if videoCodecs != "" {
		codecsStr = fmt.Sprintf(",CODECS=\"%s\"", videoCodecs)
	}
	b.WriteString(fmt.Sprintf(
		"#EXT-X-STREAM-INF:BANDWIDTH=%d%s%s\n%s\n",
		videoBandwidth, codecsStr, audioRef, videoPlaylist,
	))

	return os.WriteFile(path, []byte(b.String()), 0o644)
}
