// package clip_api provides clip-related API handlers.
package clip_api

import (
	"errors"
	"log/slog"
	"mime"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/utils/filename"
)

func HandleDownloadExport(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, _, err := sm.GetSession(c.Request())
		if err != nil {
			return c.String(401, "unauthorized")
		}

		exportIDUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		exportID := c.Param("id")

		ctx := c.Request().Context()
		q := dbc.Queries(ctx)
		exportData, err := q.GetClipExportForDownload(ctx, exportIDUUID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return c.String(404, "export not found")
			}
			return c.String(500, "failed to load export")
		}
		if exportData.Status != "ready" {
			return c.String(409, "export not ready")
		}

		if _, err := os.Stat(exportData.FilePath); err != nil {
			slog.Warn("export file missing, requeuing", "export_id", exportID, "file_path", exportData.FilePath)
			if requeueErr := q.RequeueClipExport(ctx, exportIDUUID); requeueErr != nil {
				slog.Error("failed to requeue missing export", "export_id", exportID, "error", requeueErr)
			}
			_, _ = dbc.Exec(ctx, "SELECT pg_notify('clip_exports', $1)", exportID)
			return c.String(410, "Export file was missing. It has been re-queued. Please try exporting again.")
		}

		_ = q.UpdateClipExportLastAccessed(ctx, exportIDUUID)
		cleanupClipExportsLRU(ctx, dbc)

		ext := "." + exportData.Format
		ct := mime.TypeByExtension(ext)
		if ct != "" {
			c.Response().Header().Set(echo.HeaderContentType, ct)
		}

		// Build a human-friendly download filename.
		// Pattern: "{title}[-{cropName}]-{exportID}.ext"
		// Falls back to "clip[-{cropName}]-{exportID}.ext" when clip has no title.
		titlePart := filename.Sanitize(exportData.ClipTitle, 80)
		if titlePart == "" {
			titlePart = "clip"
		}

		// Resolve crop name from variant + clip crops
		var cropSuffix string
		if strings.HasPrefix(exportData.Variant, "crop:") {
			cropID := strings.TrimPrefix(exportData.Variant, "crop:")
			for _, cr := range exportData.Crops {
				if cr.ID == cropID && cr.Name != "" {
					cropSuffix = "-" + filename.Sanitize(cr.Name, 30)
					break
				}
			}
			if cropSuffix == "" {
				cropSuffix = "-cropped"
			}
		}

		downloadName := titlePart + cropSuffix + "-" + exportID + ext
		return c.Attachment(exportData.FilePath, downloadName)
	}
}
