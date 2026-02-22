package admin

import (
	"log/slog"
	"os"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleAdminExportsDeleteByStatus(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)

		status := c.Param("status")
		if status != "ready" && status != "error" && status != "queued" {
			return c.String(400, "invalid status")
		}

		// Get file paths first (only ready exports have files)
		if status == "ready" {
			files, _ := q.ListClipExportFilesByStatus(ctx, db.ExportStatus(status))
			for _, exp := range files {
				if exp.FilePath != "" {
					_ = os.Remove(exp.FilePath)
				}
			}
		}

		// Delete DB records
		if err := q.DeleteClipExportsByStatus(ctx, db.ExportStatus(status)); err != nil {
			slog.Error("failed to delete exports by status", "error", err, "status", status)
			return c.String(500, "failed to delete exports")
		}

		slog.Info("deleted exports by status", "status", status)
		return c.Redirect(303, "/admin/exports?alert=success&msg="+status+"+exports+deleted")
	}
}

// HandleAdminExportsRequeueErrors requeues all failed exports.
