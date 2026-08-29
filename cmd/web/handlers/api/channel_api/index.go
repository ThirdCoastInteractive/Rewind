// Package channel_api serves the channels listing as a DataStar SSE fragment.
package channel_api

import (
	"log/slog"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

// HandleIndex serves GET /api/channels/index, patching the channels list
// fragment. Honors the page's channelFilter signal (name substring match).
func HandleIndex(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return echo.NewHTTPError(401, "unauthorized")
		}

		var sig struct {
			Filter string `json:"channelFilter"`
		}
		_ = datastar.ReadSignals(c.Request(), &sig)

		var filter *string
		if f := strings.TrimSpace(sig.Filter); f != "" {
			filter = &f
		}

		ctx := c.Request().Context()
		channels, err := dbc.Queries(ctx).ListChannels(ctx, filter)
		if err != nil {
			slog.Error("failed to list channels", "error", err)
			channels = nil
		}

		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		return sse.PatchElementTempl(templates.ChannelsList(channels), datastar.WithSelectorID("channels-list"))
	}
}
