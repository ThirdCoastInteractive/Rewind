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
		"--sub-lang", "en",
		"--progress",
		"--progress-delta", "5",
		"--newline",
		"--no-colors",
		"--no-video-multistreams",
		"--audio-multistreams",
		"--format", "bestvideo+mergeall/best",
	}
	args = append(args, extraArgs...)
	args = append(args, url)

	stdout, stderr, err := c.exec(ctx, args...)
	if err != nil {
		return wrapExecError(c.PathOrDefault(), args, stdout, stderr, err)
	}
	return nil
}
