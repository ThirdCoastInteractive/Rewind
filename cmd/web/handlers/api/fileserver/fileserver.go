// Package fileserver provides file serving utilities for API handlers.
package fileserver

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

// ETagMode determines how ETags are computed.
type ETagMode int

const (
	// ETagWeakStat uses file size and modtime for a weak ETag.
	ETagWeakStat ETagMode = iota
	// ETagStrongSHA256 computes a SHA256 hash of the file content.
	ETagStrongSHA256
)

// fileCacheEntry stores cached ETag info.
type fileCacheEntry struct {
	size    int64
	modTime time.Time
	mode    ETagMode
	etag    string
}

// FileCache memoizes ETags for on-disk files.
// Entries are invalidated automatically when file size or modtime changes.
type FileCache struct {
	mu      sync.RWMutex
	entries map[string]fileCacheEntry
}

// NewFileCache creates a new file cache.
func NewFileCache() *FileCache {
	return &FileCache{entries: make(map[string]fileCacheEntry)}
}

// ETag computes or retrieves a cached ETag for the given file.
func (c *FileCache) ETag(path string, info os.FileInfo, mode ETagMode) (string, error) {
	// Fast path: cached and still valid
	c.mu.RLock()
	if e, ok := c.entries[path]; ok {
		if e.size == info.Size() && e.modTime.Equal(info.ModTime()) && e.mode == mode {
			c.mu.RUnlock()
			return e.etag, nil
		}
	}
	c.mu.RUnlock()

	etag, err := computeETag(path, info, mode)
	if err != nil {
		return "", err
	}

	c.mu.Lock()
	c.entries[path] = fileCacheEntry{
		size:    info.Size(),
		modTime: info.ModTime(),
		mode:    mode,
		etag:    etag,
	}
	c.mu.Unlock()

	return etag, nil
}

func computeETag(path string, info os.FileInfo, mode ETagMode) (string, error) {
	switch mode {
	case ETagWeakStat:
		return fmt.Sprintf(`W/"%x-%x"`, info.ModTime().Unix(), info.Size()), nil
	case ETagStrongSHA256:
		f, err := os.Open(path)
		if err != nil {
			return "", err
		}
		defer f.Close()
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			return "", err
		}
		return fmt.Sprintf(`"%x"`, h.Sum(nil)), nil
	default:
		return "", fmt.Errorf("unknown etag mode: %d", mode)
	}
}

// FileServer provides file serving with caching support.
type FileServer struct {
	cache *FileCache
}

// NewFileServer creates a new file server with caching.
func NewFileServer() *FileServer {
	return &FileServer{cache: NewFileCache()}
}

// ServeDiskFileWithCache serves a file from disk with caching headers and conditional request support.
func (fs *FileServer) ServeDiskFileWithCache(c echo.Context, absPath string, contentType string, cacheControl string, etagMode ETagMode) error {
	info, err := os.Stat(absPath)
	if err != nil {
		return echo.ErrNotFound
	}

	etag := ""
	if fs.cache != nil {
		if v, err := fs.cache.ETag(absPath, info, etagMode); err == nil {
			etag = v
		}
	}

	// Conditional requests
	if etag != "" {
		if inm := c.Request().Header.Get("If-None-Match"); inm != "" && strings.TrimSpace(inm) == etag {
			return c.NoContent(http.StatusNotModified)
		}
	}
	if ims := c.Request().Header.Get(echo.HeaderIfModifiedSince); ims != "" {
		if t, err := time.Parse(time.RFC1123, ims); err == nil {
			// Round to seconds (HTTP date resolution)
			if !info.ModTime().After(t.Add(time.Second)) {
				return c.NoContent(http.StatusNotModified)
			}
		}
	}

	c.Response().Header().Set(echo.HeaderCacheControl, cacheControl)
	c.Response().Header().Set("Last-Modified", info.ModTime().UTC().Format(time.RFC1123))
	if etag != "" {
		c.Response().Header().Set("ETag", etag)
	}
	if contentType != "" {
		c.Response().Header().Set("Content-Type", contentType)
	}

	f, err := os.Open(absPath)
	if err != nil {
		return echo.ErrNotFound
	}
	defer f.Close()

	// http.ServeContent supports Range requests (used by the video player).
	name := filepath.Base(absPath)
	http.ServeContent(c.Response(), c.Request(), name, info.ModTime(), f)
	return nil
}

// GetVideoDirForID returns the directory path for a video UUID.
// It checks both /downloads (prod) and /download (dev) paths.
func GetVideoDirForID(ctx context.Context, videoID string) (string, error) {
	_ = ctx
	// Prefer /downloads (prod). Also allow /download (dev/workspaces).
	candidates := []string{
		filepath.Join(string(filepath.Separator)+"downloads", videoID),
		filepath.Join(string(filepath.Separator)+"download", videoID),
	}
	for _, dir := range candidates {
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			return dir, nil
		}
	}
	// Default to prod path and let callers 404 on missing files.
	return candidates[0], nil
}
