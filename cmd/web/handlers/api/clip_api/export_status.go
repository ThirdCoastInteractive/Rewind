package clip_api

import (
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleExportStatusStream(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, _, err := sm.GetSession(c.Request())
		if err != nil {
			return c.String(401, "unauthorized")
		}

		exportUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}

		ctx := c.Request().Context()
		q := dbc.Queries(ctx)

		// Verify export exists and get clip ID
		exportRow, err := q.GetClipExportStatus(ctx, exportUUID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return c.String(404, "export not found")
			}
			return c.String(500, "failed to get export")
		}

		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		return streamExportStatus(c, sse, dbc, exportUUID, exportRow.ClipID.String())
	}
}

// HandleBankExportStatus hydrates clip bank with current export statuses.
