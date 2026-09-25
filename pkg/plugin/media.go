package plugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var masterExts = []string{".mp4", ".webm", ".mkv", ".mov", ".avi"}

// MasterExts returns a copy of the master media file extensions.
func MasterExts() []string {
	out := make([]string, len(masterExts))
	copy(out, masterExts)
	return out
}

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

// MasterSource is a local filesystem path or an http(s) URL ffmpeg can read.
// Call cleanup when done (may be a no-op). Never copy a whole remote master
// unless Open-to-temp is the only option (no PublicURL, no LocalMaster).
func MasterSource(ctx context.Context, videoID string) (src string, cleanup func(), err error) {
	nop := func() {}
	if p, ok := LocalMaster(videoID); ok {
		return p, nop, nil
	}
	b := Blobs()
	if b == nil {
		return "", nop, fmt.Errorf("no master source for %s: blob plugin not registered", videoID)
	}
	spool := strings.TrimSpace(os.Getenv("SPOOL_DIR"))
	if spool == "" {
		spool = os.TempDir()
	}
	for _, key := range MasterKeys(videoID) {
		r, _, oerr := b.Open(ctx, key)
		if oerr != nil {
			continue
		}
		// Open first so a signer that can presign arbitrary keys cannot select
		// an absent .mp4 ahead of an existing .webm master.
		u, uerr := b.PublicURL(ctx, key, 15*time.Minute)
		if uerr == nil && strings.TrimSpace(u) != "" {
			_ = r.Close()
			return u, nop, nil
		}
		if err := os.MkdirAll(spool, 0o755); err != nil {
			_ = r.Close()
			return "", nop, fmt.Errorf("spool dir for master: %w", err)
		}
		f, ferr := os.CreateTemp(spool, "rewind-master-*")
		if ferr != nil {
			r.Close()
			return "", nop, fmt.Errorf("create temp master: %w", ferr)
		}
		tempPath := f.Name()
		_, copyErr := io.Copy(f, r)
		closeErr := f.Close()
		r.Close()
		if copyErr != nil {
			_ = os.Remove(tempPath)
			return "", nop, fmt.Errorf("spool master %s: %w", key, copyErr)
		}
		if closeErr != nil {
			_ = os.Remove(tempPath)
			return "", nop, fmt.Errorf("spool master %s: %w", key, closeErr)
		}
		return tempPath, func() { _ = os.Remove(tempPath) }, nil
	}
	return "", nop, fmt.Errorf("no master source for %s", videoID)
}

// MasterSourceAt resolves an authoritative database video_path/blob key. Live
// callers must provide a tenant scope and the key must be inside that tenant's
// org prefix; OSS callers retain arbitrary safe blob keys.
func MasterSourceAt(ctx context.Context, key string) (string, func(), error) {
	nop := func() {}
	key = strings.TrimSpace(key)
	if key == "" || strings.HasPrefix(key, "/") || strings.Contains(key, "\\") || strings.Contains(key, "..") || strings.Contains(key, "://") {
		return "", nop, fmt.Errorf("unsafe master blob key")
	}
	tenant, scoped := TenantScope(ctx)
	if LiveIngest() != nil && !scoped {
		return "", nop, fmt.Errorf("workspace tenant scope required for live master")
	}
	if scoped {
		parts := strings.Split(key, "/")
		if tenant == "" || len(parts) < 3 || parts[0] != "org" || parts[1] != tenant {
			return "", nop, fmt.Errorf("master blob key is outside workspace")
		}
	}
	b := Blobs()
	if b == nil {
		return "", nop, fmt.Errorf("blob plugin not registered")
	}
	if u, err := b.PublicURL(ctx, key, 15*time.Minute); err == nil && strings.TrimSpace(u) != "" {
		return u, nop, nil
	}
	r, _, err := b.Open(ctx, key)
	if err != nil {
		return "", nop, err
	}
	spool := strings.TrimSpace(os.Getenv("SPOOL_DIR"))
	if spool == "" {
		spool = os.TempDir()
	}
	if err := os.MkdirAll(spool, 0o755); err != nil {
		_ = r.Close()
		return "", nop, err
	}
	f, err := os.CreateTemp(spool, "rewind-master-")
	if err != nil {
		_ = r.Close()
		return "", nop, err
	}
	path := f.Name()
	_, copyErr := io.Copy(f, r)
	closeErr := f.Close()
	_ = r.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(path)
		return "", nop, fmt.Errorf("spool master: %v", copyErr)
	}
	return path, func() { _ = os.Remove(path) }, nil
}
