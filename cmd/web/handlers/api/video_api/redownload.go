// package video_api provides video-related API handlers.
package video_api

import (
	"log/slog"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/plugin"
)

// HandleRedownload creates a new download job for an existing video (force redownload).
func HandleRedownload(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return err
		}

		videoUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}

		videoRow, err := common.RequireVideo(c, dbc.Queries(c.Request().Context()), videoUUID, plugin.ActionVideoWrite)
		if err != nil {
			return err
		}

		job, err := dbc.Queries(c.Request().Context()).EnqueueDownloadJob(c.Request().Context(), &db.EnqueueDownloadJobParams{
			URL:        videoRow.Src,
			ArchivedBy: userUUID,
			Refresh:    false,
			ExtraArgs:  []string{},
		})
		if err != nil {
			slog.Error("failed to create redownload job", "error", err)
			return c.String(500, "failed to create download job")
		}

		slog.Info("created redownload job", "job_id", job.ID, "video_id", videoUUID, "url", videoRow.Src)

		return c.JSON(200, map[string]any{
			"job_id": job.ID.String(),
			"status": job.Status,
		})
	}
}
