package admin

import (
	"log/slog"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleAdminExportsRequeueErrors(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)

		if err := q.RequeueAllErrorExports(ctx); err != nil {
			slog.Error("failed to requeue error exports", "error", err)
			return c.String(500, "failed to requeue exports")
		}

		// Notify encoder workers
		_, _ = dbc.Exec(ctx, "SELECT pg_notify('clip_exports', 'requeue')")

		slog.Info("requeued all error exports")
		return c.Redirect(303, "/admin/exports?alert=success&msg=Error+exports+requeued")
	}
}

// HandleAdminExportDelete deletes a single export.
