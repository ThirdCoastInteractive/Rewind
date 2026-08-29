package ytdlp

import (
	"context"
	"fmt"
	"path/filepath"
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
		"--sub-lang", "en.*",
		"--progress",
		"--progress-delta", "5",
		"--newline",
		"--no-colors",
		"--no-video-multistreams",
		"--audio-multistreams",
		"--format", formatSelector(c.MaxHeight),
	}
	args = append(args, extraArgs...)
	args = append(args, url)

	stdout, stderr, err := c.exec(ctx, args...)
	if err != nil {
		return wrapExecError(c.PathOrDefault(), args, stdout, stderr, err)
	}
	return nil
}

// formatSelector builds the yt-dlp -f expression, optionally capped to maxHeight
// pixels of vertical resolution. maxHeight <= 0 means no cap.
//
// The primary branch takes the best video plus every audio-only (multi-language)
// stream; it falls back to the best muxed stream. Two filters make this robust:
//
//   - [vcodec=none] on mergeall restricts the merge to audio-only tracks, so we
//     add audio without pulling in extra video streams.
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
func formatSelector(maxHeight int) string {
	const noPreview = "[format_id!*=timeline]"
	if maxHeight <= 0 {
		return "bestvideo" + noPreview + "+mergeall[vcodec=none]/best"
	}
	h := maxHeight
	return fmt.Sprintf(
		"bestvideo[height<=%d]%s+mergeall[vcodec=none]/best[height<=%d]/worst",
		h, noPreview, h,
	)
}
