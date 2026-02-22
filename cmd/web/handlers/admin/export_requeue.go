package admin

import (
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleAdminExportRequeue(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)

		exportID := c.Param("id")
		var exportUUID pgtype.UUID
		if err := exportUUID.Scan(exportID); err != nil {
			return c.String(400, "invalid export id")
		}

		// Delete any existing file
		exp, err := q.GetClipExportByID(ctx, exportUUID)
		if err != nil {
			return c.String(404, "export not found")
		}
		if exp.FilePath != "" {
			_ = os.Remove(exp.FilePath)
		}

		// Requeue
		if err := q.RequeueClipExport(ctx, exportUUID); err != nil {
			slog.Error("failed to requeue export", "error", err, "id", exportID)
			return c.String(500, "failed to requeue export")
		}

		// Notify encoder workers
		_, _ = dbc.Exec(ctx, "SELECT pg_notify('clip_exports', $1)", exportID)

		return c.String(200, "requeued")
	}
}
