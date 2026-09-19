package plugin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var masterExts = []string{".mp4", ".webm", ".mkv", ".mov", ".avi"}

// MasterKeys are the blob keys that may hold a video's playable master.
func MasterKeys(videoID string) []string {
	out := make([]string, 0, len(masterExts))
	for _, ext := range masterExts {
		out = append(out, VideoKey(videoID, videoID+".video"+ext))
	}
	return out
}

// LocalRoot is the filesystem directory behind a local Blob (DOWNLOADS_DIR).
// R2 returns false.
func LocalRoot() (string, bool) {
	b := Blobs()
	if b == nil {
		return "", false
	}
	p, ok := b.LocalPath("x")
	if !ok {
		return "", false
	}
	return filepath.Dir(p), true
}

// VideoDir is the local directory for a video's files. R2 has no directory.
func VideoDir(videoID string) (string, error) {
	b := Blobs()
	if b == nil {
		return "", errors.New("blob plugin not registered")
	}
	p, ok := b.LocalPath(VideoKey(videoID, "x"))
	if !ok {
		return "", fmt.Errorf("blob has no local directory for %s", videoID)
	}
	return filepath.Dir(p), nil
}

// LocalMaster is the filesystem path of a video master when Blob is local disk.
func LocalMaster(videoID string) (string, bool) {
	b := Blobs()
	if b == nil {
		return "", false
	}
	for _, key := range MasterKeys(videoID) {
		p, ok := b.LocalPath(key)
		if !ok {
			continue
		}
		st, err := os.Stat(p)
		if err == nil && st.Mode().IsRegular() {
			return p, true
		}
	}
	return "", false
}
