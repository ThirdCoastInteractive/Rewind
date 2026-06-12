// package job_api provides download job API handlers.
package job_api

import (
	"log/slog"
	"strings"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/archival"
	"thirdcoast.systems/rewind/internal/db"
)
// HandleCreateDownload serves POST /download-jobs, enqueuing a new URL for download.
func HandleCreateDownload(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		archivedByUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return err
		}

		var req struct {
			URL string `json:"url"`
		}
		if err := c.Bind(&req); err != nil {
			return c.String(400, "invalid json")
		}
		req.URL = strings.TrimSpace(req.URL)
		if req.URL == "" {
			return c.String(400, "url is required")
		}

		res, err := archival.EnqueueURL(c.Request().Context(), dbc.Queries(c.Request().Context()), req.URL, archivedByUUID)
		if err != nil {
			slog.Error("failed to enqueue download", "error", err)
			return c.String(500, "failed to enqueue")
		}

		resp := map[string]any{
			"id":       res.Job.ID.String(),
			"status":   res.Job.Status,
			"refresh":  res.Refresh,
			"playlist": res.IsPlaylist,
		}
		return c.JSON(200, resp)
	}
}

// HandleRetry retries a failed download job.
