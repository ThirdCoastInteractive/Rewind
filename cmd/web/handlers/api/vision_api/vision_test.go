package vision_api

import (
	"github.com/labstack/echo/v4"
	"testing"
)

func TestOnlyVisualRoutesRemain(t *testing.T) {
	e := echo.New()
	Register(e, nil, nil)
	paths := map[string]bool{}
	for _, r := range e.Routes() {
		paths[r.Path] = true
	}
	for _, p := range []string{"/people", "/api/people", "/api/people/:id", "/api/people/:id/merge", "/api/faces/:id", "/api/faces/:id/thumbnail", "/api/face-index-selection", "/api/vision/events"} {
		if paths[p] {
			t.Errorf("retired route still registered: %s", p)
		}
	}
	for _, p := range []string{"/visual", "/api/visual/search", "/api/visual-index", "/api/visual-references", "/api/videos/:id/frame"} {
		if !paths[p] {
			t.Errorf("visual route missing: %s", p)
		}
	}
}
