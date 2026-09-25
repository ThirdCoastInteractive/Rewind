package fileserver

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/pkg/plugin"
)

// OpenOrRedirect opens the first blob key that exists. If that key has a
// PublicURL, the reader is closed and the URL is returned for a redirect.
func OpenOrRedirect(ctx context.Context, b plugin.Blob, keys []string) (string, plugin.Reader, string, error) {
	if b == nil {
		return "", nil, "", plugin.ErrNotFound
	}
	for _, key := range keys {
		r, _, err := b.Open(ctx, key)
		if err != nil {
			continue
		}
		if u, uerr := b.PublicURL(ctx, key, 15*time.Minute); uerr == nil && u != "" {
			_ = r.Close()
			return u, nil, key, nil
		}
		return "", r, key, nil
	}
	return "", nil, "", plugin.ErrNotFound
}

// ServeKey serves a blob object. Local disk uses the file cache; remote blobs
// are streamed from Open.
func (fs *FileServer) ServeKey(c echo.Context, key, contentType, cacheControl string, etagMode ETagMode) error {
	b := plugin.Blobs()
	if b == nil {
		return echo.ErrNotFound
	}
	if p, ok := b.LocalPath(key); ok {
		if _, err := os.Stat(p); err != nil {
			return echo.ErrNotFound
		}
		if fs == nil {
			return serveOpened(c, p, contentType, cacheControl)
		}
		return fs.ServeDiskFileWithCache(c, p, contentType, cacheControl, etagMode)
	}
	r, st, err := b.Open(c.Request().Context(), key)
	if err != nil {
		return echo.ErrNotFound
	}
	defer r.Close()
	if cacheControl != "" {
		c.Response().Header().Set(echo.HeaderCacheControl, cacheControl)
	}
	if contentType != "" {
		c.Response().Header().Set("Content-Type", contentType)
	}
	c.Response().Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(c.Response(), c.Request(), filepath.Base(key), st.ModTime, r)
	return nil
}

func serveOpened(c echo.Context, absPath, contentType, cacheControl string) error {
	f, err := os.Open(absPath)
	if err != nil {
		return echo.ErrNotFound
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return echo.ErrNotFound
	}
	if cacheControl != "" {
		c.Response().Header().Set(echo.HeaderCacheControl, cacheControl)
	}
	if contentType != "" {
		c.Response().Header().Set("Content-Type", contentType)
	}
	http.ServeContent(c.Response(), c.Request(), filepath.Base(absPath), info.ModTime(), f)
	return nil
}
