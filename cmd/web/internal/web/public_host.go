package web

import (
	"net"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

const canonicalPublicHost = "liverewind.xyz"

// redirectPublicAliases sends the old public names to liverewind.xyz.
// Health checks and provider webhooks stay on the host they were registered with.
func redirectPublicAliases(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		r := c.Request()
		host := requestHostname(r.Host)
		if host != "rewind.thirdcoast.systems" && host != "www.liverewind.xyz" {
			return next(c)
		}
		path := r.URL.Path
		if path == "/healthz" || strings.HasPrefix(path, "/webhooks/") {
			return next(c)
		}
		target := "https://" + canonicalPublicHost + r.URL.RequestURI()
		return c.Redirect(http.StatusPermanentRedirect, target)
	}
}

func requestHostname(hostport string) string {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	return strings.ToLower(strings.TrimSuffix(host, "."))
}
