// package clip_api provides clip-related API handlers.
package clip_api

import (
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/utils/crops"
)

func HandleCropCreate(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		clipIDStr := c.Param("clipId")
		clipID, err := common.RequireUUIDParam(c, "clipId")
		if err != nil {
			return err
		}

		q := dbc.Queries(ctx)
		clip, err := q.GetClip(ctx, clipID)
		if err != nil {
			return echo.NewHTTPError(http.StatusNotFound, "Clip not found")
		}

		var req struct {
			AspectRatio string   `json:"aspect_ratio"`
			Name        string   `json:"name,omitempty"`
			X           *float64 `json:"x,omitempty"`
			Y           *float64 `json:"y,omitempty"`
			Width       *float64 `json:"width,omitempty"`
			Height      *float64 `json:"height,omitempty"`
		}
		if err := c.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid request")
		}

		videoWidth := 1920
		videoHeight := 1080

		var newCrop crops.Crop
		cropID := uuid.New().String()
		cropName := req.Name
		if cropName == "" {
			cropName = fmt.Sprintf("%s Crop", req.AspectRatio)
		}

		if req.AspectRatio == "custom" && req.X != nil && req.Y != nil && req.Width != nil && req.Height != nil {
			newCrop = crops.Crop{
				ID:          cropID,
				Name:        cropName,
				AspectRatio: "custom",
				X:           *req.X,
				Y:           *req.Y,
				Width:       *req.Width,
				Height:      *req.Height,
			}
		} else {
			newCrop = crops.CalculateCropForAspectRatio(videoWidth, videoHeight, req.AspectRatio, cropID, cropName)
		}

		existingCrops := clip.Crops
		existingCrops = append(existingCrops, newCrop)

		if err := q.UpdateClipCrops(ctx, &db.UpdateClipCropsParams{ID: clipID, Crops: existingCrops}); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update clip")
		}

		sse := datastar.NewSSE(c.Response().Writer, c.Request())

		updatedClip, err := q.GetClip(ctx, clipID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to fetch updated clip")
		}

		patchCropUI(sse, clipIDStr, updatedClip.Crops)

		return nil
	}
}

// HandleCropUpdate updates an existing crop.
