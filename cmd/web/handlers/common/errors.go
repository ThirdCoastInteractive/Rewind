package common

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// ErrBadRequest returns a 400 Bad Request error.
func ErrBadRequest(msg string) *echo.HTTPError {
	return echo.NewHTTPError(http.StatusBadRequest, msg)
}

// ErrNotFound returns a 404 Not Found error.
func ErrNotFound(msg string) *echo.HTTPError {
	return echo.NewHTTPError(http.StatusNotFound, msg)
}

// ErrUnauthorized returns a 401 Unauthorized error.
func ErrUnauthorized() *echo.HTTPError {
	return echo.NewHTTPError(http.StatusUnauthorized, "unauthorized")
}

// ErrInternal returns a 500 Internal Server Error.
func ErrInternal(msg string) *echo.HTTPError {
	return echo.NewHTTPError(http.StatusInternalServerError, msg)
}
