package stitch_api

import (
	"errors"
	"mime"
	"net/http"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
)

// HandleStitchStream serves a completed stitch export as an inline video
// stream with full Range-request support (for <video> playback).
func HandleStitchStream(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return c.String(401, "unauthorized")
		}

		jobUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}

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
			return c.String(409, "stitch export not ready")
		}

		f, err := os.Open(job.FilePath)
		if err != nil {
			return c.String(410, "export file missing")
		}
		defer f.Close()

		st, err := f.Stat()
		if err != nil {
			return c.String(500, "failed to stat export file")
		}

		ext := filepath.Ext(job.FilePath)
		if ct := mime.TypeByExtension(ext); ct != "" {
			c.Response().Header().Set(echo.HeaderContentType, ct)
		}
		c.Response().Header().Set("Accept-Ranges", "bytes")

		http.ServeContent(c.Response(), c.Request(), job.FilePath, st.ModTime(), f)
		return nil
	}
}
