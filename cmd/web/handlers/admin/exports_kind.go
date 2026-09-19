package admin

import (
	"net/url"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

func exportKind(c echo.Context) string {
	if c.QueryParam("kind") == "stitch" {
		return "stitch"
	}
	return "clips"
}

func exportKindQuery(kind string) string {
	return "kind=" + url.QueryEscape(kind)
}

func clipExportStats(row *db.GetClipExportStatsRow) *templates.AdminExportStats {
	if row == nil {
		return nil
	}
	return &templates.AdminExportStats{
		QueuedCount:     row.QueuedCount,
		ProcessingCount: row.ProcessingCount,
		ReadyCount:      row.ReadyCount,
		ErrorCount:      row.ErrorCount,
		TotalSizeBytes:  row.TotalSizeBytes,
	}
}

func stitchExportStats(row *db.GetStitchExportStatsRow) *templates.AdminExportStats {
	if row == nil {
		return nil
	}
	return &templates.AdminExportStats{
		QueuedCount:     row.QueuedCount,
		ProcessingCount: row.ProcessingCount,
		ReadyCount:      row.ReadyCount,
		ErrorCount:      row.ErrorCount,
		TotalSizeBytes:  row.TotalSizeBytes,
	}
}
