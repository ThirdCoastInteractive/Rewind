package stitch_api

import (
	"log/slog"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"

	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates/components"
)

type stitchRenderSignals struct {
	StitchSegments    []components.StitchSegment `json:"_stitchSegments"`
	StitchSelectedIdx int                        `json:"_stitchSelectedIdx"`
}

// HandleRenderTimeline reads stitch signals and returns the timeline SSR fragment.
func HandleRenderTimeline() echo.HandlerFunc {
	return func(c echo.Context) error {
		signals := &stitchRenderSignals{}
		if err := datastar.ReadSignals(c.Request(), signals); err != nil {
			slog.Warn("stitch render-timeline: failed to read signals", "error", err)
			return nil
		}

		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		common.SetSSEHeaders(c)

		sse.PatchElementTempl(
			components.StitchTimeline(signals.StitchSegments, signals.StitchSelectedIdx),
			datastar.WithSelectorID("stitch-timeline"),
		)
		sse.PatchElementTempl(
			components.StitchTotalDuration(signals.StitchSegments),
			datastar.WithSelectorID("stitch-total-duration"),
		)
		return nil
	}
}

// HandleRenderDetail reads stitch signals and returns the detail panel SSR fragment.
func HandleRenderDetail() echo.HandlerFunc {
	return func(c echo.Context) error {
		signals := &stitchRenderSignals{}
		if err := datastar.ReadSignals(c.Request(), signals); err != nil {
			slog.Warn("stitch render-detail: failed to read signals", "error", err)
			return nil
		}

		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		common.SetSSEHeaders(c)

		sse.PatchElementTempl(
			components.StitchDetail(signals.StitchSegments, signals.StitchSelectedIdx),
			datastar.WithSelectorID("stitch-detail-content"),
		)
		return nil
	}
}

type stitchTransitionSignals struct {
	StitchSegments []components.StitchSegment `json:"_stitchSegments"`
	StitchTrIdx    int                        `json:"_stitchTrIdx"`
}

// HandleRenderTransitionPopup reads stitch signals and returns transition popup content.
func HandleRenderTransitionPopup() echo.HandlerFunc {
	return func(c echo.Context) error {
		signals := &stitchTransitionSignals{}
		if err := datastar.ReadSignals(c.Request(), signals); err != nil {
			slog.Warn("stitch render-transition-popup: failed to read signals", "error", err)
			return nil
		}

		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		common.SetSSEHeaders(c)

		sse.PatchElementTempl(
			components.StitchTransitionPopup(signals.StitchSegments, signals.StitchTrIdx),
			datastar.WithSelectorID("stitch-transition-popup"),
		)
		return nil
	}
}
