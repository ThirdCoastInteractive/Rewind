// package marker_api provides marker-related API handlers.
package marker_api

import (
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
)

// HandleDelete deletes a marker.
func HandleDelete(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return err
		}

		markerUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}

		existing, err := dbc.Queries(c.Request().Context()).GetMarker(c.Request().Context(), markerUUID)
		if err != nil || existing == nil {
			return c.String(404, "marker not found")
		}
		if existing.CreatedBy != userUUID {
			return c.String(403, "forbidden")
		}

		if err := dbc.Queries(c.Request().Context()).DeleteMarker(c.Request().Context(), markerUUID); err != nil {
			return c.String(500, "failed to delete marker")
		}
		return c.NoContent(204)
	}
}
