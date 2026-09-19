package common

import (
	"fmt"

	"github.com/labstack/echo/v4"
)

// SetSSEHeaders sets the complete streaming header set. Datastar sets most of
// these itself, while plain EventSource handlers need all of them.
func SetSSEHeaders(c echo.Context) {
	c.Response().Header().Set(echo.HeaderContentType, "text/event-stream")
	c.Response().Header().Set(echo.HeaderCacheControl, "no-cache")
	c.Response().Header().Set(echo.HeaderConnection, "keep-alive")
	c.Response().Header().Set("X-Accel-Buffering", "no")
}

// WriteSSEKeepalive writes an SSE comment. Comment frames are ignored by
// EventSource and DataStar, but a failed write is how we notice the client
// has gone so the handler can return and release the request goroutine.
func WriteSSEKeepalive(c echo.Context) error {
	if _, err := fmt.Fprint(c.Response(), ": keepalive\n\n"); err != nil {
		return err
	}
	c.Response().Flush()
	return nil
}
