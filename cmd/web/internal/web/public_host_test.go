package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestAliasHostsRedirectToLiveRewind(t *testing.T) {
	e := echo.New()
	e.Pre(redirectPublicAliases)
	e.Any("/*", func(c echo.Context) error { return c.NoContent(http.StatusOK) })

	ok := func(method, host, path string) {
		t.Helper()
		req := httptest.NewRequest(method, path, nil)
		req.Host = host
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s%s -> %d", method, host, path, rec.Code)
		}
	}
	gone := func(method, host, path, want string) {
		t.Helper()
		req := httptest.NewRequest(method, path, nil)
		req.Host = host
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusPermanentRedirect || rec.Header().Get("Location") != want {
			t.Fatalf("%s %s%s -> %d %q", method, host, path, rec.Code, rec.Header().Get("Location"))
		}
	}

	ok(http.MethodGet, "liverewind.xyz", "/docs/agents")
	ok(http.MethodGet, "localhost:8081", "/live")
	ok(http.MethodGet, "rewind.thirdcoast.systems", "/healthz")
	ok(http.MethodPost, "www.liverewind.xyz", "/webhooks/stripe")
	gone(http.MethodGet, "rewind.thirdcoast.systems", "/docs/agents", "https://liverewind.xyz/docs/agents")
	gone(http.MethodGet, "www.liverewind.xyz", "/start?plan=solo", "https://liverewind.xyz/start?plan=solo")
	gone(http.MethodPost, "rewind.thirdcoast.systems", "/mcp", "https://liverewind.xyz/mcp")
}
