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

// HandleAdminExportsIndex serves GET /admin/exports/index, streaming the paginated exports table via SSE.
func HandleAdminExportsIndex(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)
		kind := exportKind(c)

		page, _ := strconv.Atoi(c.QueryParam("page"))
		if page < 1 {
			page = 1
		}
		pageSize := 50
		offset := (page - 1) * pageSize

		var (
			exports []*templates.AdminExportRow
			stats   *templates.AdminExportStats
			total   int64
		)
		if kind == "stitch" {
			rows, err := q.ListStitchExportsForAdmin(ctx, &db.ListStitchExportsForAdminParams{Lim: int32(pageSize), Off: int32(offset)})
			if err != nil {
				slog.Error("failed to list stitch exports", "error", err)
				return c.String(500, "failed to list exports")
			}
			exports = make([]*templates.AdminExportRow, len(rows))
			for i, exp := range rows {
				lastError := ""
				if exp.LastError != nil {
					lastError = *exp.LastError
				}
				title := exp.Title
				if title == "" {
					title = "Untitled stitch"
				}
				projectID := ""
				if exp.ProjectID.Valid {
					projectID = exp.ProjectID.String()
				}
				exports[i] = &templates.AdminExportRow{
					Kind:        "stitch",
					ID:          exp.ID.String(),
					ProjectID:   projectID,
					Status:      string(exp.Status),
					Variant:     exp.Format + " · " + exp.Quality,
					FilePath:    exp.FilePath,
					SizeBytes:   exp.SizeBytes,
					ProgressPct: exp.ProgressPct,
					Attempts:    exp.Attempts,
					LastError:   lastError,
					CreatedAt:   exp.CreatedAt.Time.Format("2006-01-02 15:04"),
					Title:       title,
				}
			}
			stats = stitchExportStats(mustStitchStats(q.GetStitchExportStats(ctx)))
			total, _ = q.CountStitchExports(ctx)
		} else {
			dbExports, err := q.ListClipExportsForAdmin(ctx, &db.ListClipExportsForAdminParams{
				Lim: int32(pageSize),
				Off: int32(offset),
			})
			if err != nil {
				slog.Error("failed to list clip exports", "error", err)
				return c.String(500, "failed to list exports")
			}
			exports = make([]*templates.AdminExportRow, len(dbExports))
			for i, exp := range dbExports {
				lastError := ""
				if exp.LastError != nil {
					lastError = *exp.LastError
				}
				exports[i] = &templates.AdminExportRow{
					Kind:         "clip",
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
					Title:        exp.ClipLabel,
				}
			}
			if row, err := q.GetClipExportStats(ctx); err == nil {
				stats = clipExportStats(row)
			}
			total, _ = q.CountClipExports(ctx)
		}

		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		if err := sse.PatchElementTempl(templates.AdminExportsTable(kind, exports, stats, int(total), page, pageSize)); err != nil {
			slog.Error("failed to patch exports table", "error", err)
		}
		return nil
	}
}

func mustStitchStats(row *db.GetStitchExportStatsRow, err error) *db.GetStitchExportStatsRow {
	if err != nil {
		return nil
	}
	return row
}
