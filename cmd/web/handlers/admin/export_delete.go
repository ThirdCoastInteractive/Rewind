package admin

import (
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/internal/db"
)

// HandleAdminExportDelete serves DELETE /admin/exports/:id, removing a rendered export file and row.
func HandleAdminExportDelete(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)

		var exportUUID pgtype.UUID
		if err := exportUUID.Scan(c.Param("id")); err != nil {
			return c.String(400, "invalid export id")
		}

		if exportKind(c) == "stitch" {
			job, err := q.GetStitchJob(ctx, exportUUID)
			if err != nil {
				return c.String(404, "export not found")
			}
			if job.FilePath != "" {
				_ = os.Remove(job.FilePath)
			}
			if err := q.DeleteStitchJob(ctx, exportUUID); err != nil {
				slog.Error("failed to delete stitch export", "error", err, "id", c.Param("id"))
				return c.String(500, "failed to delete export")
			}
			return c.String(200, "deleted")
		}

		exp, err := q.GetClipExportByID(ctx, exportUUID)
		if err != nil {
			return c.String(404, "export not found")
		}
		if exp.FilePath != "" {
			_ = os.Remove(exp.FilePath)
		}
		if err := q.DeleteClipExport(ctx, exportUUID); err != nil {
			slog.Error("failed to delete clip export", "error", err, "id", c.Param("id"))
			return c.String(500, "failed to delete export")
		}
		return c.String(200, "deleted")
	}
}
