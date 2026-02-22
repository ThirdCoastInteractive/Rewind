package clip_api

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
)

// HandleSplit splits a clip at the given timestamp into two clips.
// POST /api/clips/:id/split  { "at": <float64> }
func HandleSplit(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()

		userUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return err
		}

		clipUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}

		var req struct {
			At float64 `json:"at"`
		}
		if err := c.Bind(&req); err != nil {
			return c.String(400, "invalid json")
		}

		existing, err := dbc.Queries(ctx).GetClip(ctx, clipUUID)
		if err != nil || existing == nil {
			return c.String(404, "clip not found")
		}

		// Validate: split point must be strictly inside the clip with at least
		// a tiny margin (~1 frame at 60fps ≈ 0.016s) from either edge.
		const minGap = 0.016
		if req.At <= existing.StartTs+minGap || req.At >= existing.EndTs-minGap {
			return c.String(400, "split point must be inside the clip bounds")
		}

		// Perform both operations in a transaction.
		qtx, tx, err := dbc.NewWithTX(ctx)
		if err != nil {
			return c.String(500, "failed to start transaction")
		}
		defer tx.Rollback(ctx)

		// 1. Shrink the original clip: end → split point.
		newEnd := req.At
		newDur := newEnd - existing.StartTs
		_, err = qtx.UpdateClip(ctx, &db.UpdateClipParams{
			ID:       clipUUID,
			EndTs:    &newEnd,
			Duration: &newDur,
		})
		if err != nil {
			return c.String(500, "failed to update original clip")
		}

		// 2. Create a new clip from split point → original end.
		secondStart := req.At
		secondEnd := existing.EndTs
		secondDur := secondEnd - secondStart

		secondTitle := existing.Title
		if secondTitle != "" {
			secondTitle = secondTitle + " (split)"
		}

		tags := existing.Tags
		if len(tags) == 0 {
			tags = []byte("[]")
		}

		newClip, err := qtx.CreateClip(ctx, &db.CreateClipParams{
			VideoID:     existing.VideoID,
			StartTs:     secondStart,
			EndTs:       secondEnd,
			Duration:    secondDur,
			Title:       secondTitle,
			Description: existing.Description,
			Color:       existing.Color,
			Tags:        tags,
			CreatedBy:   userUUID,
		})
		if err != nil {
			return c.String(500, "failed to create split clip")
		}

		// Copy filter stack if present.
		if len(existing.FilterStack) > 0 {
			_ = copyFilterStack(ctx, qtx, newClip.ID, existing.FilterStack)
		}

		if err := tx.Commit(ctx); err != nil {
			return c.String(500, "failed to commit split")
		}

		return c.JSON(200, map[string]string{
			"original_id": existing.ID.String(),
			"new_id":      newClip.ID.String(),
		})
	}
}

// copyFilterStack copies the filter stack JSON to the new clip.
func copyFilterStack(ctx context.Context, qtx *db.Queries, clipID pgtype.UUID, stack json.RawMessage) error {
	return qtx.UpdateClipFilterStack(ctx, &db.UpdateClipFilterStackParams{
		ID:          clipID,
		FilterStack: stack,
	})
}
