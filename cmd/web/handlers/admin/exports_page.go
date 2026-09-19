package admin

import (
	"log/slog"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

// HandleAdminExportsPage serves GET /admin/exports with clip and stitch queues split.
func HandleAdminExportsPage(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		username, _ := c.Get("currentUsername").(string)
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)
		kind := exportKind(c)

		clipCount, _ := q.CountClipExports(ctx)
		stitchCount, _ := q.CountStitchExports(ctx)

		var stats *templates.AdminExportStats
		if kind == "stitch" {
			row, err := q.GetStitchExportStats(ctx)
			if err != nil {
				slog.Error("failed to get stitch export stats", "error", err)
			} else {
				stats = stitchExportStats(row)
			}
		} else {
			row, err := q.GetClipExportStats(ctx)
			if err != nil {
				slog.Error("failed to get clip export stats", "error", err)
			} else {
				stats = clipExportStats(row)
			}
		}

		return templates.AdminExports(username, kind, clipCount, stitchCount, stats, c.QueryParam("alert"), c.QueryParam("msg")).Render(ctx, c.Response().Writer)
	}
}
