package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestSkipGzipDownloadsAndStreams(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/stitch/abc/download", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if !skipGzip(c) {
		t.Fatal("stitch download should skip gzip")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/videos/x/stream", nil)
	c = e.NewContext(req, rec)
	if !skipGzip(c) {
		t.Fatal("video stream should skip gzip")
	}

	req = httptest.NewRequest(http.MethodGet, "/stitch/abc", nil)
	c = e.NewContext(req, rec)
	if skipGzip(c) {
		t.Fatal("html stitch page should gzip")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/videos/x/stream", nil)
	req.Header.Set("Range", "bytes=0-1")
	c = e.NewContext(req, rec)
	if !skipGzip(c) {
		t.Fatal("range requests should skip gzip")
	}
}
