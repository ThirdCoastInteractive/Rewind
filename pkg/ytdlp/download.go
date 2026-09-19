package ytdlp

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// Download downloads the media and writes a matching .info.json into destDir.
// It uses a stable output template so ingest can discover pairs:
//
//	<destDir>/<extractor>_<id>.<ext>
//	<destDir>/<extractor>_<id>.info.json
//
// This fetches the video, metadata, thumbnails, subtitles/captions, chapters,
// and descriptions as recommended by yt-dlp best practices.
func (c *Client) Download(ctx context.Context, url string, destDir string, extraArgs ...string) error {
	if strings.TrimSpace(url) == "" {
		return fmt.Errorf("ytdlp: url is required")
	}
	if strings.TrimSpace(destDir) == "" {
		return fmt.Errorf("ytdlp: destDir is required")
	}

	// Use the actual extension from yt-dlp so the filename matches the produced file.
	// If we later remux, yt-dlp will update %(ext)s accordingly.
	// Include media_type to make spool/debug filenames more informative (e.g., clip/trailer/episode).
	// Ingest will rename assets to UUID-based deterministic names in permanent storage.
	tmpl := filepath.Join(destDir, "%(extractor)s_%(id)s_%(media_type)s.%(ext)s")

	args := []string{
		"-o", tmpl,
		"--remux-video", "mp4",
		"--fixup", "force",
		"--write-info-json",
		"--write-thumbnail",
		"--write-subs",
		"--write-auto-subs",
		// Regex, anchored by yt-dlp as `^en.*$`. Plain "en" would miss regional and
		// auto-generated variants (e.g. Rumble exposes its captions as "en-auto",
		// YouTube uses "en-US"/"en-orig") — "en.*" catches every English track.
		"--sub-lang", "en.*,live_chat",
		"--progress",
		"--progress-delta", "5",
		"--newline",
		"--no-colors",
		"--no-video-multistreams",
		"--no-audio-multistreams",
		"--format", formatSelector(c.MaxHeight, c.Live),
	}
	args = append(args, extraArgs...)
	args = append(args, url)

	stdout, stderr, err := c.exec(ctx, args...)
	if err != nil {
		return wrapExecError(c.PathOrDefault(), args, stdout, stderr, err)
	}
	return nil
}

// LiveOpts configures yt-dlp flags for live HLS/EVENT streams.
type LiveOpts struct {
	FromStart bool
	Wait      int // seconds; 0 means omit --wait-for-video
}

// LiveDownloadArgs returns yt-dlp flags for native HLS live capture.
// Does not include -f/--format; live format selection goes through Client.Live.
func LiveDownloadArgs(opts LiveOpts) []string {
	args := make([]string, 0, 8)
	if opts.FromStart {
		args = append(args, "--live-from-start")
	} else {
		args = append(args, "--no-live-from-start")
	}
	args = append(args,
		"--downloader", "native",
		"--hls-use-mpegts",
		"--skip-unavailable-fragments",
	)
	if opts.Wait > 0 {
		args = append(args, "--wait-for-video", strconv.Itoa(opts.Wait))
	}
	return args
}

// StripRewindExtraArgs removes --rewind-media-url <url> pairs.
// Returns the media URL (empty if none) and the remaining args.
func StripRewindExtraArgs(extra []string) (mediaURL string, rest []string) {
	rest = make([]string, 0, len(extra))
	for i := 0; i < len(extra); i++ {
		if extra[i] == "--rewind-media-url" {
			if i+1 < len(extra) {
				mediaURL = extra[i+1]
				i++
			}
			continue
		}
		rest = append(rest, extra[i])
	}
	return mediaURL, rest
}

// ParseLiveExtraArgs extracts live-related flags from extra args.
// Remaining args exclude --live-from-start, --no-live-from-start, and
// --wait-for-video <n> so LiveDownloadArgs can re-add them without duplicates.
func ParseLiveExtraArgs(extra []string) (opts LiveOpts, hasLiveHint bool, rest []string) {
	rest = make([]string, 0, len(extra))
	for i := 0; i < len(extra); i++ {
		switch extra[i] {
		case "--live-from-start":
			opts.FromStart = true
			hasLiveHint = true
			continue
		case "--no-live-from-start":
			opts.FromStart = false
			hasLiveHint = true
			continue
		case "--wait-for-video":
			hasLiveHint = true
			if i+1 < len(extra) {
				if n, err := strconv.Atoi(extra[i+1]); err == nil {
					opts.Wait = n
				}
				i++
			}
			continue
		}
		rest = append(rest, extra[i])
	}
	return opts, hasLiveHint, rest
}

// formatSelector builds the yt-dlp -f expression, optionally capped to maxHeight
// pixels of vertical resolution. maxHeight <= 0 means no cap.
//
// When live is true, a muxed stream is required: bestvideo+bestaudio merge fails
// on HLS EVENT playlists (ffmpeg from fragment 0). Use best, or best[height<=N]/worst.
//
// For VOD, the primary branch takes the best video plus the best audio stream; it
// falls back to the best muxed stream. The preview filter makes this robust:
//
//   - [format_id!*=timeline] excludes Rumble's "timeline-*" seekbar preview — a
//     ~180p clip that is the only strict video-only format Rumble exposes. Left
//     in, it hijacks bestvideo: yt-dlp merges it with whatever audio track is
//     present and "succeeds" at 180p, never reaching /best. Excluding it makes
//     the bestvideo branch miss on such sources so selection falls through to the
//     real muxed renditions. The guard is a no-op on sites without such a format.
//
// When capped, the two primary branches carry a [height<=N] filter and select
// the HIGHEST rendition at or below the limit. Only if a source has nothing at
// or below the cap does the trailing /worst fire — grabbing the smallest
// rendition available (least overage) so the video is still archived. That beats
// a /best fallback, which would grab the largest and blow past the cap the most.
func formatSelector(maxHeight int, live bool) string {
	if live {
		if maxHeight <= 0 {
			return "best"
		}
		return fmt.Sprintf("best[height<=%d]/worst", maxHeight)
	}
	const noPreview = "[format_id!*=timeline]"
	if maxHeight <= 0 {
		return "bestvideo" + noPreview + "+bestaudio/best"
	}
	h := maxHeight
	return fmt.Sprintf(
		"bestvideo[height<=%d]%s+bestaudio/best[height<=%d]/worst",
		h, noPreview, h,
	)
}
