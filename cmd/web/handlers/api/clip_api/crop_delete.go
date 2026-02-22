// package clip_api provides clip-related API handlers.
package clip_api

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/utils/crops"
)

func HandleCropDelete(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
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

		existingCrops := clip.Crops
		var updatedCrops crops.CropArray
		var found bool
		for i := range existingCrops {
			if existingCrops[i].ID == cropID {
				found = true
				continue
			}
			updatedCrops = append(updatedCrops, existingCrops[i])
		}

		if !found {
			return echo.NewHTTPError(http.StatusNotFound, "Crop not found")
		}

		if err := q.UpdateClipCrops(ctx, &db.UpdateClipCropsParams{ID: clipUUID, Crops: updatedCrops}); err != nil {
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
