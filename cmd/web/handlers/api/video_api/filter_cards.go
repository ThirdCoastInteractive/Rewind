package video_api

import (
	"log/slog"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/templates/components"
	"thirdcoast.systems/rewind/pkg/filters"
)

// HandleFilterCards renders the server-side filter card list and patches it
// into the DOM via SSE. This is a render-only endpoint - it does NOT persist
// filter_stack to the database. Persistence goes through the unified clip
// SAVE button → HandleUpdate.
//
// The _filterStack signal (array of {type, params}) is read from the request.
// Because the signal has an underscore prefix, the frontend must explicitly
// include it via filterSignals:{include:/_filterStack|_selectedClipId/} on the
// @post() call.
func HandleFilterCards() echo.HandlerFunc {
	return func(c echo.Context) error {
		// IMPORTANT: ReadSignals MUST happen BEFORE NewSSE.
		// NewSSE flushes response headers which closes the request body.
		type Signals struct {
			FilterStack []filters.FilterStackEntry `json:"_filterStack"`
		}
		signals := &Signals{}
		if err := datastar.ReadSignals(c.Request(), signals); err != nil {
			slog.Warn("failed to read filter-cards signals", "error", err)
			return nil
		}

		sse := datastar.NewSSE(c.Response().Writer, c.Request())

		videoID := c.Param("id")

		// Crop options for the crop filter. For now, a single placeholder
		// option is provided. A future enhancement can read crops from the
		// database when a clip is selected.
		cropOptions := []filters.FilterOption{
			{Value: "", Label: "(select crop)"},
		}

		sse.PatchElementTempl(
			components.FilterCardList(signals.FilterStack, videoID, cropOptions),
			datastar.WithSelectorID("filter-stack-list"),
		)

		return nil
	}
}
