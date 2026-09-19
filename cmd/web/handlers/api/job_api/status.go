package job_api

import (
	"log/slog"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)
// HandleStatus serves GET /jobs/:id/status, streaming real-time job progress updates via SSE.
func HandleStatus(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		jobUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}

		// Set up SSE
		common.SetSSEHeaders(c)

		sse := datastar.NewSSE(c.Response().Writer, c.Request())

		ctx := c.Request().Context()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		keepalive := time.NewTicker(15 * time.Second)
		defer keepalive.Stop()
		timeout := time.NewTimer(2 * time.Hour)
		defer timeout.Stop()

		var lastStatus db.JobStatus

		for {
			select {
			case <-ctx.Done():
				slog.Info("SSE connection closed by client", "job_id", jobUUID)
				return nil
			case <-timeout.C:
				slog.Warn("SSE connection timeout", "job_id", jobUUID)
				return nil
			case <-keepalive.C:
				if err := common.WriteSSEKeepalive(c); err != nil {
					return nil
				}
			case <-ticker.C:
				job, err := dbc.Queries(ctx).GetDownloadJobByID(ctx, jobUUID)
				if err != nil {
					slog.Error("failed to fetch job for SSE", "error", err, "job_id", jobUUID)
					return err
				}

				if job.Status != lastStatus {
					lastStatus = job.Status
					if err := sse.PatchElementTempl(templates.JobDetailCard(job)); err != nil {
						slog.Error("failed to send SSE patch", "error", err, "job_id", jobUUID)
						return err
					}
					if job.Status == db.JobStatusSucceeded || job.Status == db.JobStatusFailed {
						slog.Info("Job finished, closing SSE connection", "job_id", jobUUID, "status", job.Status)
						return nil
					}
				}
			}
		}
	}
}
