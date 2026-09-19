package admin

import (
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/internal/db"
)

// HandleAdminExportRequeue serves POST /admin/exports/:id/requeue.
func HandleAdminExportRequeue(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
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
			if err := q.RequeueStitchJob(ctx, exportUUID); err != nil {
				slog.Error("failed to requeue stitch export", "error", err, "id", c.Param("id"))
				return c.String(500, "failed to requeue export")
			}
			_, _ = dbc.Exec(ctx, "SELECT pg_notify('stitch_jobs', $1)", c.Param("id"))
			return c.String(200, "requeued")
		}

		exp, err := q.GetClipExportByID(ctx, exportUUID)
		if err != nil {
			return c.String(404, "export not found")
		}
		if exp.FilePath != "" {
			_ = os.Remove(exp.FilePath)
		}
		if err := q.RequeueClipExport(ctx, exportUUID); err != nil {
			slog.Error("failed to requeue clip export", "error", err, "id", c.Param("id"))
			return c.String(500, "failed to requeue export")
		}
		_, _ = dbc.Exec(ctx, "SELECT pg_notify('clip_exports', $1)", c.Param("id"))
		return c.String(200, "requeued")
	}
}
