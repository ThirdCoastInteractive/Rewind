package content

import (
	"log/slog"
	"strings"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/archival"
	"thirdcoast.systems/rewind/internal/db"
)

// HandleArchiveSubmit serves POST /archive from the home-page URL form. It
// enqueues the submitted URL (expanding playlists/channels) and redirects to
// the job page (single video) or the jobs dashboard (playlist, where the child
// jobs appear).
func HandleArchiveSubmit(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		archivedByUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}

		url := strings.TrimSpace(c.FormValue("url"))
		if url == "" {
			return c.Redirect(302, "/")
		}

		res, err := archival.EnqueueURL(c.Request().Context(), dbc.Queries(c.Request().Context()), url, archivedByUUID)
		if err != nil {
			slog.Error("failed to enqueue from home form", "error", err, "url", url)
			return c.Redirect(302, "/jobs")
		}

		if res.IsPlaylist {
			return c.Redirect(302, "/jobs")
		}
		return c.Redirect(302, "/jobs/"+res.Job.ID.String())
	}
}
