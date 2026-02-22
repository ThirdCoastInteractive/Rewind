package admin

import (
	"log/slog"
	"os"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleAdminExportsDeleteAll(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)

		// Get all file paths first
		readyFiles, _ := q.ListClipExportFilesByStatus(ctx, "ready")

		// Delete files
		deletedFiles := 0
		for _, exp := range readyFiles {
			if exp.FilePath != "" {
				if err := os.Remove(exp.FilePath); err == nil {
					deletedFiles++
				}
			}
		}

		// Delete all DB records
		if err := q.DeleteAllClipExports(ctx); err != nil {
			slog.Error("failed to delete all exports", "error", err)
			return c.String(500, "failed to delete exports")
		}

		slog.Info("deleted all exports", "files_deleted", deletedFiles)

		// Redirect back to refresh
		return c.Redirect(303, "/admin/exports?alert=success&msg=All+exports+deleted")
	}
}

// HandleAdminExportsDeleteByStatus deletes exports by status.
