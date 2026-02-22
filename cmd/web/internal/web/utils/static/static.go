package static

import (
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/static"
)

// CachedFileInfo holds metadata for a static file used in HTTP cache headers.
type CachedFileInfo struct {
	ETag         string
	Size         int64
	LastModified time.Time
}

// StaticCache manages in-memory metadata for static assets.
type StaticCache struct {
	fileLock sync.RWMutex
	entries  map[string]CachedFileInfo
	fs       fs.FS
}

// NewStaticCache scans the embedded filesystem and computes ETag and Last-Modified for each file.
func NewStaticCache() (*StaticCache, error) {
	c := &StaticCache{
		entries: make(map[string]CachedFileInfo),
		fs:      static.FS,
	}

	c.fileLock.Lock()
	defer c.fileLock.Unlock()

	err := fs.WalkDir(static.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		f, err := static.FS.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		info, err := f.Stat()
		if err != nil {
			return err
		}

		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			return err
		}
		etag := fmt.Sprintf("\"%x\"", h.Sum(nil))
		modTime := info.ModTime()
		if modTime.IsZero() {
			modTime = time.Now()
		}

		c.entries[path] = CachedFileInfo{
			ETag:         etag,
			Size:         info.Size(),
			LastModified: modTime,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (s *StaticCache) ServeStaticFile(prefix string) echo.HandlerFunc {
	return func(c echo.Context) error {
		path := strings.TrimPrefix(c.Request().URL.Path, "/static/")

		// Check cache for file metadata
		s.fileLock.RLock()
		ci, ok := s.entries[path]
		s.fileLock.RUnlock()

		// If client has up-to-date version, return 304
		if ok {
			if inm := c.Request().Header.Get("If-None-Match"); inm != "" && inm == ci.ETag {
				return c.NoContent(http.StatusNotModified)
			}
			if ims := c.Request().Header.Get(echo.HeaderIfModifiedSince); ims != "" {
				if t, err := time.Parse(time.RFC1123, ims); err == nil && ci.LastModified.Before(t.Add(time.Second)) {
					return c.NoContent(http.StatusNotModified)
				}
			}
		}

		// Set cache header based on file type
		var cacheHeader string
		ext := filepath.Ext(path)

		// NOTE: /static/dist/* assets are not fingerprinted, so long-lived caching can
		// easily result in stale JS/CSS (especially behind Cloudflare).
		if strings.HasPrefix(path, "dist/") && (ext == ".css" || ext == ".js") {
			cacheHeader = "no-cache, must-revalidate"
		} else {
			switch ext {
			case ".css", ".js":
				cacheHeader = "public, max-age=86400, stale-while-revalidate=3600" // 1 day
			case ".png", ".jpg", ".jpeg", ".gif", ".svg", ".ico":
				cacheHeader = "public, max-age=31536000, stale-while-revalidate=86400" // 1 year
			case ".woff", ".woff2", ".ttf":
				cacheHeader = "public, max-age=31536000, stale-while-revalidate=86400" // 1 year
			default:
				cacheHeader = "public, max-age=3600, stale-while-revalidate=300" // 1 hour
			}
		}
		c.Response().Header().Set(echo.HeaderCacheControl, cacheHeader)

		// Open and serve the file
		f, err := static.FS.Open(path)
		if err != nil {
			return echo.ErrNotFound
		}
		defer f.Close()

		if ok {
			c.Response().Header().Set("ETag", ci.ETag)
			c.Response().Header().Set(echo.HeaderLastModified, ci.LastModified.Format(time.RFC1123))
		}

		contentType := mime.TypeByExtension(ext)
		if contentType == "" {
			contentType = "application/octet-stream"
		}

		return c.Stream(http.StatusOK, contentType, f)

	}
}
