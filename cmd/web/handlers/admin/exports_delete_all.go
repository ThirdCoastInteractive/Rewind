package admin

import (
	"log/slog"
	"os"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/internal/db"
)

// HandleAdminExportsDeleteAll serves POST /admin/exports/delete-all for the selected queue.
func HandleAdminExportsDeleteAll(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)
		kind := exportKind(c)

		if kind == "stitch" {
			files, _ := q.ListStitchExportFilesByStatus(ctx, db.ExportStatusReady)
			for _, exp := range files {
				if exp.FilePath != "" {
					_ = os.Remove(exp.FilePath)
				}
			}
			if err := q.DeleteAllStitchExports(ctx); err != nil {
				slog.Error("failed to delete all stitch exports", "error", err)
				return c.String(500, "failed to delete exports")
			}
			return c.Redirect(303, "/admin/exports?"+exportKindQuery(kind)+"&alert=success&msg=Stitch+exports+deleted")
		}

		readyFiles, _ := q.ListClipExportFilesByStatus(ctx, db.ExportStatusReady)
		for _, exp := range readyFiles {
			if exp.FilePath != "" {
				_ = os.Remove(exp.FilePath)
			}
		}
		if err := q.DeleteAllClipExports(ctx); err != nil {
			slog.Error("failed to delete all clip exports", "error", err)
			return c.String(500, "failed to delete exports")
		}
		return c.Redirect(303, "/admin/exports?"+exportKindQuery(kind)+"&alert=success&msg=Clip+exports+deleted")
	}
}
