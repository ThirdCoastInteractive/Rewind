// package marker_api provides marker-related API handlers.
package marker_api

import (
	"strings"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
)

// HandleCreateOrUpdate updates an existing marker.
func HandleCreateOrUpdate(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, _, err := common.RequireSessionUser(c, sm)
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

		var req struct {
			Timestamp   *float64 `json:"timestamp"`
			Title       *string  `json:"title"`
			Description *string  `json:"description"`
			Color       *string  `json:"color"`
			MarkerType  *string  `json:"marker_type"`
		}
		if err := c.Bind(&req); err != nil {
			return c.String(400, "invalid json")
		}

		var tsPtr *float64
		if req.Timestamp != nil {
			if *req.Timestamp < 0 {
				return c.String(400, "timestamp must be >= 0")
			}
			tsPtr = req.Timestamp
		}

		var markerType db.NullMarkerType
		if req.MarkerType != nil {
			markerType = db.NullMarkerType{
				MarkerType: db.MarkerType(strings.TrimSpace(*req.MarkerType)),
				Valid:      true,
			}
		}

		updated, err := dbc.Queries(c.Request().Context()).UpdateMarker(c.Request().Context(), &db.UpdateMarkerParams{
			ID:          markerUUID,
			Timestamp:   tsPtr,
			Title:       req.Title,
			Description: req.Description,
			Color:       req.Color,
			MarkerType:  markerType,
		})
		if err != nil {
			return c.String(500, "failed to update marker")
		}

		return c.JSON(200, updated)
	}
}
