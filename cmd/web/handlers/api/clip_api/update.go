// package clip_api provides clip-related API handlers.
package clip_api

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates/components"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleUpdate(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
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

		existing, err := dbc.Queries(ctx).GetClip(ctx, clipUUID)
		if err != nil || existing == nil {
			return c.String(404, "clip not found")
		}
		if existing.CreatedBy != userUUID {
			return c.String(403, "forbidden")
		}

		body, _ := io.ReadAll(c.Request().Body)
		c.Request().Body = io.NopCloser(bytes.NewReader(body))

		// Read signals from DataStar request.
		// All clip state is persisted here via the explicit SAVE button.
		// This is the single write path for clips - no auto-save, no race.
		type Signals struct {
			ClipTitle       string          `json:"clipTitle"`
			ClipDescription string          `json:"clipDescription"`
			ClipColor       string          `json:"clipColor"`
			FilterStack     json.RawMessage `json:"_filterStack"`
			ClipStartTs     *float64        `json:"_clipStartTs"`
			ClipEndTs       *float64        `json:"_clipEndTs"`
		}

		signals := &Signals{}
		_ = datastar.ReadSignals(c.Request(), signals)

		// Read JSON payload (e.g., clip timing updates)
		c.Request().Body = io.NopCloser(bytes.NewReader(body))
		var req struct {
			StartTs *float64 `json:"start_ts"`
			EndTs   *float64 `json:"end_ts"`
		}
		_ = json.NewDecoder(c.Request().Body).Decode(&req)

		// Convert to pointers for COALESCE to work
		var title *string
		var description *string
		var color *string
		var filterStack []byte

		if signals.ClipTitle != "" {
			title = &signals.ClipTitle
		}
		if signals.ClipDescription != "" {
			description = &signals.ClipDescription
		}
		if signals.ClipColor != "" {
			color = &signals.ClipColor
		}
		// Only write filter_stack if the signal is present and non-null.
		// An empty array [] is a valid value ("no filters").
		if signals.FilterStack != nil {
			filterStack = signals.FilterStack
		}

		var startPtr, endPtr, durationPtr *float64

		// Timing can come from signals (_clipStartTs/_clipEndTs) or JSON payload.
		// Prefer signals (set by the unified SAVE model), fall back to payload.
		if signals.ClipStartTs != nil {
			startPtr = signals.ClipStartTs
		} else if req.StartTs != nil {
			startPtr = req.StartTs
		}
		if signals.ClipEndTs != nil {
			endPtr = signals.ClipEndTs
		} else if req.EndTs != nil {
			endPtr = req.EndTs
		}

		// Update duration if timing changes are present
		if req.StartTs != nil || req.EndTs != nil {
			startVal := existing.StartTs
			endVal := existing.EndTs
			if req.StartTs != nil {
				startVal = *req.StartTs
			}
			if req.EndTs != nil {
				endVal = *req.EndTs
			}
			if endVal > startVal {
				dur := endVal - startVal
				durationPtr = &dur
			}
		}

		// Update clip in database.
		// This is the single write path for all clip state.
		updatedClip, err := dbc.Queries(ctx).UpdateClip(ctx, &db.UpdateClipParams{
			ID:          clipUUID,
			StartTs:     startPtr,
			EndTs:       endPtr,
			Duration:    durationPtr,
			Title:       title,
			Description: description,
			Color:       color,
			Tags:        nil,
			FilterStack: filterStack,
		})
		if err != nil {
			return c.String(500, "failed to update clip")
		}

		// SSE response
		common.SetSSEHeaders(c)

		sse := datastar.NewSSE(c.Response().Writer, c.Request())

		// Fetch updated clips list to refresh the sidebar
		clips, err := dbc.Queries(ctx).ListClipsByVideo(ctx, updatedClip.VideoID)
		if err != nil {
			clips = []*db.Clip{}
		}

		// Determine variant from referer
		variant := "cut"
		referer := c.Request().Referer()
		if len(referer) > 0 && !strings.Contains(referer, "/cut") {
			variant = "watch"
		}

		// Patch clip list with update - the JS MutationObserver on [data-clip-list]
		// will automatically reload clip data for the timeline.
		_ = sse.PatchElementTempl(
			components.ClipList(clips, variant),
			datastar.WithSelector("[data-clip-list]"),
			datastar.WithModeReplace(),
		)

		// Re-hydrate export status badges that were wiped by the full list replace.
		PatchClipExportStatuses(sse, ctx, dbc, clips)

		// Clear dirty flag after successful save
		dirtyJSON, _ := json.Marshal(map[string]interface{}{
			"_clipDirty":   false,
			"_clipStartTs": updatedClip.StartTs,
			"_clipEndTs":   updatedClip.EndTs,
		})
		_ = sse.PatchSignals(dirtyJSON)

		return nil
	}
}

// HandleSelect selects a clip in the cut editor.
