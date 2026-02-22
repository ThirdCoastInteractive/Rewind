package job_api

import (
	"log/slog"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleIndex(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		// Auth required
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return c.String(401, "unauthorized")
		}

		ctx := c.Request().Context()
		rows, err := dbc.Queries(ctx).ListRecentDownloadJobs(ctx)
		if err != nil {
			slog.Error("failed to fetch jobs for SSE", "error", err)
			rows = []*db.DownloadJob{}
		}

		// Set up SSE
		common.SetSSEHeaders(c)

		sse := datastar.NewSSE(c.Response().Writer, c.Request())

		fragment := templates.JobsList(rows)
		if err := sse.PatchElementTempl(fragment); err != nil {
			slog.Error("failed to send jobs list SSE patch", "error", err)
			return err
		}

		return nil
	}
}
