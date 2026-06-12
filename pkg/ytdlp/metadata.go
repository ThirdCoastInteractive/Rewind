package ytdlp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DumpInfoJSON writes yt-dlp's --dump-single-json output to destPath.
// This is useful for refresh jobs where we don't want to download media.
func (c *Client) DumpInfoJSON(ctx context.Context, url string, destPath string, extraArgs ...string) error {
	if strings.TrimSpace(url) == "" {
		return fmt.Errorf("ytdlp: url is required")
	}
	if strings.TrimSpace(destPath) == "" {
		return fmt.Errorf("ytdlp: destPath is required")
	}

	info, err := c.GetInfo(ctx, url, extraArgs...)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}

	// Keep exact raw JSON (already trimmed in GetInfo).
	return os.WriteFile(destPath, info.Raw, 0o644)
}

// WriteComments asks yt-dlp to write comments json into destDir.
// Not all extractors support comments; callers may treat failures as best-effort.
func (c *Client) WriteComments(ctx context.Context, url string, destDir string, extraArgs ...string) error {
	if strings.TrimSpace(url) == "" {
		return fmt.Errorf("ytdlp: url is required")
	}
	if strings.TrimSpace(destDir) == "" {
		return fmt.Errorf("ytdlp: destDir is required")
	}

	tmpl := filepath.Join(destDir, "%(extractor)s_%(id)s.%(ext)s")

	args := []string{
		"--skip-download",
		"--write-comments",
		// max_comments is max-comments,max-parents,max-replies,max-replies-per-thread.
		// A bare total cap (the old 2500,all,all,all) gets fully consumed by
		// top-level comments on large videos, so reply threads were never fetched.
		// Allow a larger total and bound replies-per-thread so threads come in too.
		"--extractor-args", "youtube:max_comments=4000,all,all,8",
		"-o", tmpl,
	}
	args = append(args, extraArgs...)
	args = append(args, url)

	stdout, stderr, err := c.exec(ctx, args...)
	if err != nil {
		return wrapExecError(c.PathOrDefault(), args, stdout, stderr, err)
	}
	return nil
}

// WriteThumbnail asks yt-dlp to download the thumbnail into destDir.
// Not all extractors support thumbnails; callers may treat failures as best-effort.
func (c *Client) WriteThumbnail(ctx context.Context, url string, destDir string, extraArgs ...string) error {
	if strings.TrimSpace(url) == "" {
		return fmt.Errorf("ytdlp: url is required")
	}
	if strings.TrimSpace(destDir) == "" {
		return fmt.Errorf("ytdlp: destDir is required")
	}

	tmpl := filepath.Join(destDir, "%(extractor)s_%(id)s.%(ext)s")

	args := []string{
		"--skip-download",
		"--write-thumbnail",
		"-o", tmpl,
	}
	args = append(args, extraArgs...)
	args = append(args, url)

	stdout, stderr, err := c.exec(ctx, args...)
	if err != nil {
		return wrapExecError(c.PathOrDefault(), args, stdout, stderr, err)
	}
	return nil
}

// WriteSubtitles asks yt-dlp to download subtitles/auto-captions into destDir.
// This is best-effort; many sources may not have captions.
// Downloads all available languages.
func (c *Client) WriteSubtitles(ctx context.Context, url string, destDir string, extraArgs ...string) error {
	if strings.TrimSpace(url) == "" {
		return fmt.Errorf("ytdlp: url is required")
	}
	if strings.TrimSpace(destDir) == "" {
		return fmt.Errorf("ytdlp: destDir is required")
	}

	tmpl := filepath.Join(destDir, "%(extractor)s_%(id)s.%(ext)s")

	args := []string{
		"--skip-download",
		"--write-subs",
		"--write-auto-subs",
		"--sub-lang", "en",
		"-o", tmpl,
	}
	args = append(args, extraArgs...)
	args = append(args, url)

	stdout, stderr, err := c.exec(ctx, args...)
	if err != nil {
		return wrapExecError(c.PathOrDefault(), args, stdout, stderr, err)
	}
	return nil
}
