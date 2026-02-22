package admin

import (
	"github.com/labstack/echo/v4"
)

func HandleAdminSettingsPage() echo.HandlerFunc {
	return func(c echo.Context) error {
		return c.Redirect(302, "/settings")
	}
}

// HandleAdminSettings updates admin-level instance settings.
