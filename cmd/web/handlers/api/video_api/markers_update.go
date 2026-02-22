// package video_api provides video-related API handlers.
package video_api

import (
	"strings"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
)

// HandleMarkersUpdate creates a new marker for a video.
func HandleMarkersUpdate(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return err
		}

		videoUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}

		videoRow, err := dbc.Queries(c.Request().Context()).GetVideoByID(c.Request().Context(), videoUUID)
		if err != nil || videoRow == nil {
			return c.String(404, "video not found")
		}

		var req struct {
			Timestamp   float64 `json:"timestamp"`
			Title       string  `json:"title"`
			Description string  `json:"description"`
			Color       string  `json:"color"`
			MarkerType  string  `json:"marker_type"`
		}
		if err := c.Bind(&req); err != nil {
			return c.String(400, "invalid json")
		}
		if req.Timestamp < 0 {
			return c.String(400, "timestamp must be >= 0")
		}
		markerType := strings.TrimSpace(req.MarkerType)
		if markerType == "" {
			markerType = "point"
		}
		if markerType != "point" && markerType != "chapter" {
			return c.String(400, "invalid marker_type")
		}
		markerTypeEnum := db.MarkerType(markerType)
		color := strings.TrimSpace(req.Color)
		if color == "" {
			color = "#3b82f6"
		}

		created, err := dbc.Queries(c.Request().Context()).CreateMarker(c.Request().Context(), &db.CreateMarkerParams{
			VideoID:     videoUUID,
			Timestamp:   req.Timestamp,
			Title:       req.Title,
			Description: req.Description,
			Color:       color,
			MarkerType:  markerTypeEnum,
			CreatedBy:   userUUID,
		})
		if err != nil {
			return c.String(500, "failed to create marker")
		}

		return c.JSON(200, created)
	}
}
