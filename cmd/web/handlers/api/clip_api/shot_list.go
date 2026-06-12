package clip_api

import (
	"encoding/json"
	"net/http"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/utils/crops"
)

// HandleShotListUpdate serves PUT /clips/:clipId/shot-list.
func HandleShotListUpdate(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.String(401, "unauthorized")
		}

		clipUUID, err := common.RequireUUIDParam(c, "clipId")
		if err != nil {
			return err
		}

		ctx := c.Request().Context()
		q := dbc.Queries(ctx)

		clip, err := q.GetClip(ctx, clipUUID)
		if err != nil {
			return echo.NewHTTPError(http.StatusNotFound, "Clip not found")
		}

		var req struct {
			Shots []crops.Shot `json:"shots"`
		}
		if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
		}

		shotList := crops.ShotList(req.Shots)
		if len(shotList) > 0 {
			if err := shotList.Validate(clip.StartTs, clip.EndTs, clip.Crops); err != nil {
				return echo.NewHTTPError(http.StatusBadRequest, err.Error())
			}
		}

		if err := q.UpdateClipShotList(ctx, &db.UpdateClipShotListParams{
			ID:       clipUUID,
			ShotList: shotList,
		}); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to save shot list")
		}

		return c.JSON(http.StatusOK, map[string]any{
			"ok":     true,
			"shots":  shotList,
			"count":  len(shotList),
		})
	}
}
