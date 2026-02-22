// package clip_api provides clip-related API handlers.
package clip_api

import (
	"encoding/json"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/cmd/web/templates/components"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/filters"
)

func HandleSelect(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()

		clipUUID, err := common.RequireUUIDParam(c, "clipId")
		if err != nil {
			return err
		}

		videoID := c.Param("videoId")

		// SSE headers
		common.SetSSEHeaders(c)

		sse := datastar.NewSSE(c.Response().Writer, c.Request())

		q := dbc.Queries(ctx)
		clip, err := q.GetClip(ctx, clipUUID)
		if err != nil || clip == nil {
			return c.String(404, "clip not found")
		}

		// Patch the inspector form with clip data
		_ = sse.PatchElementTempl(
			templates.ClipInspectorForm(clip),
			datastar.WithSelector("[data-cut-clip-form]"),
			datastar.WithModeReplace(),
		)

		// Inspector visibility is now driven by data-show="$_selectedClipId"
		// on the template elements - no need for ExecuteScript to toggle classes.

		// Load persisted filter stack from database
		var filterStack []filters.FilterStackEntry
		if len(clip.FilterStack) > 0 {
			_ = json.Unmarshal(clip.FilterStack, &filterStack)
		}
		if filterStack == nil {
			filterStack = []filters.FilterStackEntry{}
		}

		// Patch the _filterStack, _selectedClipId, timing, and dirty flag signals
		fsJSON, _ := json.Marshal(map[string]interface{}{
			"_filterStack":    filterStack,
			"_selectedClipId": clip.ID.String(),
			"_clipDirty":      false,
			"_clipStartTs":    clip.StartTs,
			"_clipEndTs":      clip.EndTs,
		})
		_ = sse.PatchSignals(fsJSON)

		// Re-render filter cards to match loaded filter stack
		cropOptions := []filters.FilterOption{
			{Value: "", Label: "(select crop)"},
		}
		_ = sse.PatchElementTempl(
			components.FilterCardList(filterStack, videoID, cropOptions),
			datastar.WithSelectorID("filter-stack-list"),
		)

		// Re-render export panel with this clip's crop variants
		_ = sse.PatchElementTempl(
			components.CutExportPanel(clip.Crops),
			datastar.WithSelectorID("cut-export-panel"),
		)

		// The JS signal poller for _selectedClipId will automatically call
		// selectClipById() which seeks the video and centers the work window.

		return nil
	}
}

// HandleSeek seeks to a clip's start time in watch interface.
