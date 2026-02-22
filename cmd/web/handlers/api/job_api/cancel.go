// package job_api provides download job API handlers.
package job_api

import (
	"log/slog"
	"os"
	"syscall"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleCancel(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return err
		}

		jobUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}

		jobRow, err := dbc.Queries(c.Request().Context()).GetDownloadJobByID(c.Request().Context(), jobUUID)
		if err != nil || jobRow == nil {
			return c.String(404, "job not found")
		}

		// Get the PID if process is running
		pid, err := dbc.Queries(c.Request().Context()).GetDownloadJobPID(c.Request().Context(), jobUUID)
		if err == nil && pid != nil && *pid > 0 {
			process, findErr := os.FindProcess(int(*pid))
			if findErr != nil {
				slog.Warn("failed to find process", "job_id", jobUUID, "pid", *pid, "error", findErr)
			} else {
				if sigErr := process.Signal(syscall.SIGTERM); sigErr != nil {
					slog.Warn("failed to signal process", "job_id", jobUUID, "pid", *pid, "error", sigErr)
				} else {
					slog.Info("signaled process to terminate", "job_id", jobUUID, "pid", *pid)
				}
			}
		}

		if err := dbc.Queries(c.Request().Context()).CancelDownloadJob(c.Request().Context(), jobUUID); err != nil {
			slog.Error("failed to cancel job", "job_id", jobUUID, "error", err)
			return c.String(500, "failed to cancel job")
		}

		return c.JSON(200, map[string]any{"status": "cancelled"})
	}
}

// HandleArchive archives a download job.
