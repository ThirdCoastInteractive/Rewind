package content

import (
	"github.com/labstack/echo/v4"

	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

// HandleShowNoteLivePage renders the producer live control surface for a show
// note (owner/host only): scene preview, webcam grid, director, and go-live.
func HandleShowNoteLivePage(_ *auth.SessionManager, _ *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		return c.Redirect(302, "/show-notes/"+c.Param("id")+"?layout=directing")
	}
}

// HandleShowViewerPage renders the public program output for a live show note,
// resolved by its public code. No auth — the code is the access capability.
func HandleShowViewerPage(dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		code := c.Param("code")
		note, err := dbc.Queries(c.Request().Context()).GetShowNoteByPublicCode(c.Request().Context(), &code)
		if err != nil || !note.IsLive {
			return c.String(404, "Show not found")
		}
		return templates.ShowViewerPage(note.ID.String(), note.Title).Render(c.Request().Context(), c.Response())
	}
}
