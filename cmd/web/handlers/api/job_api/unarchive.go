// package job_api provides download job API handlers.
package job_api

import (
	"log/slog"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleUnarchive(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return err
		}

		jobUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}

		jobRow, err := dbc.Queries(c.Request().Context()).GetDownloadJobByID(c.Request().Context(), jobUUID)
		if err != nil || jobRow == nil {
			return c.String(404, "job not found")
		}

		if err := dbc.Queries(c.Request().Context()).UnarchiveJob(c.Request().Context(), jobUUID); err != nil {
			slog.Error("failed to unarchive job", "job_id", jobUUID, "error", err)
			return c.String(500, "failed to unarchive job")
		}

		return c.JSON(200, map[string]any{"archived": false})
	}
}

// HandleArchiveBatch archives multiple download jobs.
