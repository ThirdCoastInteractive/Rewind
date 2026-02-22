package admin

import (
	"log/slog"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleAdminExportsPage(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		username, _ := c.Get("currentUsername").(string)
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)

		dbStats, err := q.GetClipExportStats(ctx)
		if err != nil {
			slog.Error("failed to get export stats", "error", err)
			return templates.AdminExports(username, nil, "", "").Render(ctx, c.Response().Writer)
		}

		stats := &templates.AdminExportStats{
			QueuedCount:     dbStats.QueuedCount,
			ProcessingCount: dbStats.ProcessingCount,
			ReadyCount:      dbStats.ReadyCount,
			ErrorCount:      dbStats.ErrorCount,
			TotalSizeBytes:  dbStats.TotalSizeBytes,
		}

		alertType := c.QueryParam("alert")
		alertMsg := c.QueryParam("msg")

		return templates.AdminExports(username, stats, alertType, alertMsg).Render(ctx, c.Response().Writer)
	}
}

// HandleAdminExportsIndex returns the exports list via SSE.
