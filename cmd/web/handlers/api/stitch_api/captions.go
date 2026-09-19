package stitch_api

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/stitch"
)

// HandleStitchCaptions serves the immutable sidecar captured by a ready,
// owned canonical render. The format is selected from the queued options, not
// from an arbitrary filesystem path supplied by the caller.
func HandleStitchCaptions(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		owner, _, err := editorUser(c, sm, dbc)
		if err != nil {
			return c.String(401, "unauthorized")
		}
		jobID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		format := c.QueryParam("format")
		if format != "srt" && format != "vtt" {
			return c.String(400, "caption format must be srt or vtt")
		}
		job, err := stitch.NewStore(dbc).GetRenderJob(c.Request().Context(), owner, jobID)
		if err != nil {
			if errors.Is(err, stitch.ErrNotFound) || errors.Is(err, pgx.ErrNoRows) {
				return c.String(404, "stitch job not found")
			}
			return c.String(500, "failed to load stitch job")
		}
		if job.Status != "ready" {
			return c.String(409, "stitch job not ready")
		}
		if job.Options.CaptionMode != format {
			return c.String(404, "caption sidecar not available")
		}
		path := job.FilePath + "." + format
		if filepath.Dir(path) != filepath.Dir(job.FilePath) {
			return c.String(404, "caption sidecar not available")
		}
		if _, err := os.Stat(path); err != nil {
			return c.String(410, "caption sidecar missing")
		}
		if format == "srt" {
			c.Response().Header().Set(echo.HeaderContentType, "application/x-subrip; charset=utf-8")
		} else {
			c.Response().Header().Set(echo.HeaderContentType, "text/vtt; charset=utf-8")
		}
		return c.File(path)
	}
}
