package admin

import (
	"log/slog"
	"os"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/internal/db"
)

// HandleAdminExportsDeleteByStatus serves POST /admin/exports/delete/:status for the selected queue.
func HandleAdminExportsDeleteByStatus(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)
		kind := exportKind(c)

		status := c.Param("status")
		if status != "ready" && status != "error" && status != "queued" {
			return c.String(400, "invalid status")
		}
		st := db.ExportStatus(status)

		if kind == "stitch" {
			if status == "ready" {
				files, _ := q.ListStitchExportFilesByStatus(ctx, st)
				for _, exp := range files {
					if exp.FilePath != "" {
						_ = os.Remove(exp.FilePath)
					}
				}
			}
			if err := q.DeleteStitchExportsByStatus(ctx, st); err != nil {
				slog.Error("failed to delete stitch exports by status", "error", err, "status", status)
				return c.String(500, "failed to delete exports")
			}
			return c.Redirect(303, "/admin/exports?"+exportKindQuery(kind)+"&alert=success&msg="+status+"+stitch+exports+deleted")
		}

		if status == "ready" {
			files, _ := q.ListClipExportFilesByStatus(ctx, st)
			for _, exp := range files {
				if exp.FilePath != "" {
					_ = os.Remove(exp.FilePath)
				}
			}
		}
		if err := q.DeleteClipExportsByStatus(ctx, st); err != nil {
			slog.Error("failed to delete clip exports by status", "error", err, "status", status)
			return c.String(500, "failed to delete exports")
		}
		return c.Redirect(303, "/admin/exports?"+exportKindQuery(kind)+"&alert=success&msg="+status+"+clip+exports+deleted")
	}
}
