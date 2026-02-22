package admin

import (
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleAdminExportDelete(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)

		exportID := c.Param("id")
		var exportUUID pgtype.UUID
		if err := exportUUID.Scan(exportID); err != nil {
			return c.String(400, "invalid export id")
		}

		// Get file path first
		exp, err := q.GetClipExportByID(ctx, exportUUID)
		if err != nil {
			return c.String(404, "export not found")
		}

		// Delete file if exists
		if exp.FilePath != "" {
			_ = os.Remove(exp.FilePath)
		}

		// Delete DB record
		if err := q.DeleteClipExport(ctx, exportUUID); err != nil {
			slog.Error("failed to delete export", "error", err, "id", exportID)
			return c.String(500, "failed to delete export")
		}

		return c.String(200, "deleted")
	}
}

// HandleAdminExportRequeue requeues a single export.
