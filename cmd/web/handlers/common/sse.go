package common

import "github.com/labstack/echo/v4"

// SetSSEHeaders sets headers needed for SSE that datastar.NewSSE() does NOT set.
// datastar already sets Content-Type, Cache-Control, and Connection.
// This only adds X-Accel-Buffering for nginx/reverse proxy compatibility.
func SetSSEHeaders(c echo.Context) {
	c.Response().Header().Set("X-Accel-Buffering", "no")
}
