package content

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/archival"
	"thirdcoast.systems/rewind/internal/db"
)

// HandleBookmarklet serves GET /bookmarklet, enqueuing the ?url= for download
// (playlist/channel URLs are expanded) and redirecting to its job page.
func HandleBookmarklet(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		archivedByUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}

		url := strings.TrimSpace(c.QueryParam("url"))
		if url == "" {
			return c.String(400, "url parameter is required")
		}

		res, err := archival.EnqueueURL(c.Request().Context(), dbc.Queries(c.Request().Context()), url, archivedByUUID)
		if err != nil {
			slog.Error("failed to enqueue job from bookmarklet", "error", err, "url", url)
			return c.String(500, "Failed to create download job")
		}

		jobID := res.Job.ID.String()
		slog.Info("job created from bookmarklet", "job_id", jobID, "url", url, "playlist", res.IsPlaylist)
		return c.Redirect(302, fmt.Sprintf("/jobs/%s", jobID))
	}
}
