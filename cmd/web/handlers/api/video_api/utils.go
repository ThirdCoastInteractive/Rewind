package video_api

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/pkg/plugin"
)

// VideoExtensions returns extensions to check for video files, in priority order.
// mp4 is preferred (current remux target), with fallbacks for legacy videos.
var VideoExtensions = plugin.MasterExts()

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

// removeLocalDir reports whether delete_disk should remove a local directory.
// A missing directory is not a reason to refuse the delete: Live masters are
// R2 objects, and purgeGeneratedMedia already removes those.
func removeLocalDir(deleteDisk, hasSafeDir bool) bool {
	return deleteDisk && hasSafeDir
}

// safeVideoDirForDeletion returns the blob-local directory for a video if it
// is a real directory under the Disk root and named as the video UUID.
func safeVideoDirForDeletion(videoUUID pgtype.UUID) (string, bool) {
	id := videoUUID.String()
	root, ok := plugin.LocalRoot()
	if !ok {
		return "", false
	}
	dir, err := plugin.VideoDir(id)
	if err != nil {
		return "", false
	}
	dir = filepath.Clean(dir)
	root = filepath.Clean(root)
	if filepath.Base(dir) != id {
		return "", false
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return "", false
	}
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return "", false
	}
	return dir, true
}
