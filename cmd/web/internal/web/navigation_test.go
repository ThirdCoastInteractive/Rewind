package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/templates"
)

func TestPageNavigationPreservesShell(t *testing.T) {
	for _, partial := range []bool{false, true} {
		e := echo.New()
		e.Use(pageNavigation)
		e.GET("/videos", func(c echo.Context) error {
			// Flushed templ output must remain buffered until a complete page is ready.
			c.Response().Flush()
			return templates.Layout("Library", "tester").Render(c.Request().Context(), c.Response())
		})
		r := httptest.NewRequest(http.MethodGet, "/videos", nil)
		if partial {
			r.Header.Set("X-Rewind-Navigation", "true")
		}
		w := httptest.NewRecorder()
		e.ServeHTTP(w, r)
		body := w.Body.String()
		if !strings.Contains(body, `id="page-content"`) {
			t.Fatalf("missing page content: %s", body)
		}
		if partial {
			if !strings.Contains(w.Header().Get("Content-Type"), "text/event-stream") {
				t.Fatal("expected SSE")
			}
			if strings.Contains(body, `id="rewind-agent"`) || strings.Contains(body, "<html") {
				t.Fatal("navigation replaced the persistent shell")
			}
			if !strings.Contains(body, "beforeSwap()") || !strings.Contains(body, "afterSwap()") {
				t.Fatal("missing lifecycle hooks")
			}
		} else if !strings.Contains(body, `id="rewind-agent"`) {
			t.Fatal("direct MPA load must include assistant")
		}
	}
}

func TestPageNavigationErrorAndRedirect(t *testing.T) {
	for _, status := range []int{403, 500, 302} {
		e := echo.New()
		e.Use(pageNavigation)
		e.GET("/videos", func(c echo.Context) error {
			if status == 302 {
				return c.Redirect(status, "/login")
			}
			return c.String(status, "private error details")
		})
		r := httptest.NewRequest("GET", "/videos", nil)
		r.Header.Set("X-Rewind-Navigation", "true")
		w := httptest.NewRecorder()
		e.ServeHTTP(w, r)
		body := w.Body.String()
		if strings.Contains(body, "beforeSwap") || strings.Contains(body, "private error") {
			t.Fatal("failed navigation must retain the current page")
		}
		if status == 302 && !strings.Contains(body, "redirect") {
			t.Fatal("missing redirect")
		}
	}
}

func TestNavigationPathExcludesDataAndSessionEndpoints(t *testing.T) {
	for _, path := range []string{"/api/videos/id/stream", "/static/dist/main.js", "/logout", "/login", "/mcp", "/videos-malicious"} {
		if navigationPath(path) {
			t.Fatalf("unexpected navigation route %s", path)
		}
	}
}
