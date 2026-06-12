package stitch_api

import (
	"errors"
	"mime"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/utils/filename"
)

// HandleStitchDownload serves the finished stitch file as an attachment.
func HandleStitchDownload(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return c.String(401, "unauthorized")
		}

		jobUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		jobIDStr := c.Param("id")

		ctx := c.Request().Context()
		q := dbc.Queries(ctx)

		job, err := q.GetStitchJob(ctx, jobUUID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return c.String(404, "stitch job not found")
			}
			return c.String(500, "failed to load stitch job")
		}
		if job.Status != db.ExportStatusReady {
			return c.String(409, "stitch job not ready")
		}

		if _, err := os.Stat(job.FilePath); err != nil {
			return c.String(410, "export file missing")
		}

		_ = q.UpdateStitchJobLastAccessed(ctx, jobUUID)

		ext := "." + job.Format
		if ct := mime.TypeByExtension(ext); ct != "" {
			c.Response().Header().Set(echo.HeaderContentType, ct)
		}

		titlePart := filename.Sanitize(job.Title, 80)
		if titlePart == "" {
			titlePart = "stitch"
		}
		downloadName := titlePart + "-" + jobIDStr + ext
		return c.Attachment(job.FilePath, downloadName)
	}
}
