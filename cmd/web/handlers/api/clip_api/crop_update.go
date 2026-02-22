// package clip_api provides clip-related API handlers.
package clip_api

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleCropUpdate(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		clipIDStr := c.Param("clipId")
		cropID := c.Param("cropId")
		clipUUID, err := common.RequireUUIDParam(c, "clipId")
		if err != nil {
			return err
		}

		q := dbc.Queries(ctx)
		clip, err := q.GetClip(ctx, clipUUID)
		if err != nil {
			return echo.NewHTTPError(http.StatusNotFound, "Clip not found")
		}

		var req struct {
			Name   *string  `json:"name,omitempty"`
			X      *float64 `json:"x,omitempty"`
			Y      *float64 `json:"y,omitempty"`
			Width  *float64 `json:"width,omitempty"`
			Height *float64 `json:"height,omitempty"`
		}
		if err := c.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid request")
		}

		existingCrops := clip.Crops
		var found bool
		for i := range existingCrops {
			if existingCrops[i].ID == cropID {
				found = true
				if req.Name != nil {
					existingCrops[i].Name = *req.Name
				}
				if req.X != nil {
					existingCrops[i].X = *req.X
				}
				if req.Y != nil {
					existingCrops[i].Y = *req.Y
				}
				if req.Width != nil {
					existingCrops[i].Width = *req.Width
				}
				if req.Height != nil {
					existingCrops[i].Height = *req.Height
				}
				break
			}
		}

		if !found {
			return echo.NewHTTPError(http.StatusNotFound, "Crop not found")
		}

		if err := q.UpdateClipCrops(ctx, &db.UpdateClipCropsParams{ID: clipUUID, Crops: existingCrops}); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update clip")
		}

		sse := datastar.NewSSE(c.Response().Writer, c.Request())

		updatedClip, err := q.GetClip(ctx, clipUUID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to fetch updated clip")
		}

		patchCropUI(sse, clipIDStr, updatedClip.Crops)

		return nil
	}
}

// HandleCropDelete deletes a crop.
