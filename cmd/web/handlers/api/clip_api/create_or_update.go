// package clip_api provides clip-related API handlers.
package clip_api

import (
	"encoding/json"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleCreateOrUpdate(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return err
		}

		clipUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}

		existing, err := dbc.Queries(c.Request().Context()).GetClip(c.Request().Context(), clipUUID)
		if err != nil || existing == nil {
			return c.String(404, "clip not found")
		}

		var req struct {
			StartTs     *float64         `json:"start_ts"`
			EndTs       *float64         `json:"end_ts"`
			Title       *string          `json:"title"`
			Description *string          `json:"description"`
			Color       *string          `json:"color"`
			Tags        *json.RawMessage `json:"tags"`
		}
		if err := c.Bind(&req); err != nil {
			return c.String(400, "invalid json")
		}

		var startPtr, endPtr, durPtr *float64

		startVal := -1.0
		endVal := -1.0
		if req.StartTs != nil {
			if *req.StartTs < 0 {
				return c.String(400, "start_ts must be >= 0")
			}
			startVal = *req.StartTs
			startPtr = req.StartTs
		}
		if req.EndTs != nil {
			if *req.EndTs < 0 {
				return c.String(400, "end_ts must be >= 0")
			}
			endVal = *req.EndTs
			endPtr = req.EndTs
		}
		if req.StartTs != nil || req.EndTs != nil {
			if startVal < 0 {
				startVal = existing.StartTs
			}
			if endVal < 0 {
				endVal = existing.EndTs
			}
			if endVal <= startVal {
				return c.String(400, "end_ts must be > start_ts")
			}
			durVal := endVal - startVal
			durPtr = &durVal
		}

		var tags []byte
		if req.Tags != nil {
			tags = *req.Tags
			var tmp any
			if err := json.Unmarshal(tags, &tmp); err != nil {
				return c.String(400, "invalid tags")
			}
		}

		updated, err := dbc.Queries(c.Request().Context()).UpdateClip(c.Request().Context(), &db.UpdateClipParams{
			ID:          clipUUID,
			StartTs:     startPtr,
			EndTs:       endPtr,
			Duration:    durPtr,
			Title:       req.Title,
			Description: req.Description,
			Color:       req.Color,
			Tags:        tags,
		})
		if err != nil {
			return c.String(500, "failed to update clip")
		}

		return c.JSON(200, updated)
	}
}

// HandleDelete deletes a clip.
