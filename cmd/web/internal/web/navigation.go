package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/ctxkeys"
)

type pageResponse struct {
	bytes.Buffer
	header http.Header
	status int
}

func (r *pageResponse) Header() http.Header    { return r.header }
func (r *pageResponse) WriteHeader(status int) { r.status = status }
func (r *pageResponse) Flush()                 {} // Templ may flush fragments into this buffer.

// pageNavigation uses the existing authenticated MPA handlers to render SSE pages.
// API, media, session, and non-GET requests retain their original HTTP semantics.
func pageNavigation(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		r := c.Request()
		if r.Method == http.MethodGet && navigationPath(r.URL.Path) {
			c.Response().Header().Add("Vary", "X-Rewind-Navigation")
		}
		if r.Method != http.MethodGet || r.Header.Get("X-Rewind-Navigation") != "true" || !navigationPath(r.URL.Path) {
			return next(c)
		}
		original := c.Response()
		defer c.SetResponse(original)
		capture := &pageResponse{header: make(http.Header), status: http.StatusOK}
		c.SetResponse(echo.NewResponse(capture, c.Echo()))
		c.SetRequest(r.WithContext(context.WithValue(r.Context(), ctxkeys.PageNavigation, true)))
		err := next(c)
		c.SetResponse(original)
		for key, values := range capture.header {
			if key == "Content-Type" || key == "Content-Length" || key == "Location" {
				continue
			}
			for _, value := range values {
				original.Header().Add(key, value)
			}
		}
		sse := datastar.NewSSE(original, r)
		if location := capture.header.Get("Location"); location != "" {
			encoded, _ := json.Marshal(location)
			return sse.ExecuteScript("window.RewindNavigation.redirect(" + string(encoded) + ")")
		}
		if err != nil || capture.status >= 400 {
			return sse.ExecuteScript("window.RewindNavigation.fail('Unable to load this page. Your current page is still open.')")
		}
		if !bytes.Contains(capture.Bytes(), []byte(`id="page-content"`)) {
			return sse.ExecuteScript("window.RewindNavigation.hardLoad()")
		}
		if err := sse.ExecuteScript("window.RewindNavigation.beforeSwap()"); err != nil {
			return err
		}
		if err := sse.PatchElements(capture.String(), datastar.WithSelectorID("page-content"), datastar.WithModeReplace()); err != nil {
			return err
		}
		return sse.ExecuteScript("window.RewindNavigation.afterSwap()")
	}
}

func navigationPath(path string) bool {
	if path == "/" {
		return true
	}
	for _, prefix := range []string{"/videos", "/channels", "/creators", "/follows", "/visual", "/network", "/wiki", "/compilations", "/stitch", "/show-notes", "/jobs", "/upload", "/settings", "/admin", "/producer"} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}
