package video_api

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

// VideoExtensions returns extensions to check for video files, in priority order.
// mp4 is preferred (current remux target), with fallbacks for legacy videos.
var VideoExtensions = []string{".mp4", ".webm", ".mkv"}

// Regex patterns for seek-related parameters
var (
	ReSeekLevelParam = regexp.MustCompile(`^[a-z0-9_-]+$`)
	ReSeekSheetParam = regexp.MustCompile(`^seek-[0-9]{3}\.jpg$`)
)

// isTruthyQueryParam returns true if the query param value represents a truthy value.
func isTruthyQueryParam(v string) bool {
	s := strings.TrimSpace(strings.ToLower(v))
	return s == "1" || s == "true" || s == "yes" || s == "on"
}

// safeVideoDirForDeletion validates and returns the video directory path for safe deletion.
// Returns the directory path and true if valid, or empty string and false if invalid.
func safeVideoDirForDeletion(videoUUID pgtype.UUID) (string, bool) {
	// We derive the on-disk directory from the UUID. Still enforce: {parent=downloads|download}/{uuid}
	candidates := []string{
		filepath.Join(string(filepath.Separator)+"downloads", videoUUID.String()),
		filepath.Join(string(filepath.Separator)+"download", videoUUID.String()),
	}
	for _, dir := range candidates {
		dir = filepath.Clean(dir)
		if filepath.Base(dir) != videoUUID.String() {
			continue
		}
		parent := strings.ToLower(strings.TrimSpace(filepath.Base(filepath.Dir(dir))))
		if parent != "downloads" && parent != "download" {
			continue
		}
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			return dir, true
		}
	}

	return "", false
}
