package content

import (
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleJobDetailPage(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, username, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}

		jobUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}

		dbJob, err := dbc.Queries(c.Request().Context()).GetDownloadJobByID(c.Request().Context(), jobUUID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return c.String(404, "Job not found")
			}
			slog.Error("failed to fetch job", "error", err)
			return c.String(500, "Failed to fetch job")
		}

		return templates.JobDetail(dbJob, username).Render(c.Request().Context(), c.Response())
	}
}
