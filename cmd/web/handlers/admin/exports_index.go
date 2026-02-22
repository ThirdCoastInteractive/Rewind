package admin

import (
	"log/slog"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleAdminExportsIndex(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)

		page, _ := strconv.Atoi(c.QueryParam("page"))
		if page < 1 {
			page = 1
		}
		pageSize := 50
		offset := (page - 1) * pageSize

		dbExports, err := q.ListClipExportsForAdmin(ctx, &db.ListClipExportsForAdminParams{
			Lim: int32(pageSize),
			Off: int32(offset),
		})
		if err != nil {
			slog.Error("failed to list exports", "error", err)
			return c.String(500, "failed to list exports")
		}

		// Convert to template types
		exports := make([]*templates.AdminExportRow, len(dbExports))
		for i, exp := range dbExports {
			lastError := ""
			if exp.LastError != nil {
				lastError = *exp.LastError
			}
			exports[i] = &templates.AdminExportRow{
				ID:           exp.ID.String(),
				ClipID:       exp.ClipID.String(),
				VideoID:      exp.VideoID.String(),
				Status:       string(exp.Status),
				Variant:      string(exp.Variant),
				FilePath:     exp.FilePath,
				SizeBytes:    exp.SizeBytes,
				ProgressPct:  exp.ProgressPct,
				Attempts:     exp.Attempts,
				LastError:    lastError,
				CreatedAt:    exp.CreatedAt.Time.Format("2006-01-02 15:04"),
				ClipLabel:    exp.ClipLabel,
				VideoTitle:   exp.VideoTitle,
				ClipDuration: exp.ClipDuration,
			}
		}

		dbStats, _ := q.GetClipExportStats(ctx)
		var stats *templates.AdminExportStats
		if dbStats != nil {
			stats = &templates.AdminExportStats{
				QueuedCount:     dbStats.QueuedCount,
				ProcessingCount: dbStats.ProcessingCount,
				ReadyCount:      dbStats.ReadyCount,
				ErrorCount:      dbStats.ErrorCount,
				TotalSizeBytes:  dbStats.TotalSizeBytes,
			}
		}
		total, _ := q.CountClipExports(ctx)

		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		if err := sse.PatchElementTempl(templates.AdminExportsTable(exports, stats, int(total), page, pageSize)); err != nil {
			slog.Error("failed to patch exports table", "error", err)
		}
		return nil
	}
}

// HandleAdminExportsDeleteAll deletes all exports and their files.
