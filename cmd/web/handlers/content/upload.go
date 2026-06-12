package content

import (
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/templates"
)
// HandleUploadPage serves GET /upload, rendering the local file upload page.
func HandleUploadPage(sm *auth.SessionManager) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, username, err := sm.GetSession(c.Request())
		if err != nil {
			return c.Redirect(302, "/login")
		}

		return templates.Upload(username).Render(c.Request().Context(), c.Response())
	}
}
