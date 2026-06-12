package video_api

import (
	"log/slog"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)
// HandleRecent serves GET /videos/recent, returning recently added videos via SSE.
func HandleRecent(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return c.String(401, "unauthorized")
		}

		ctx := c.Request().Context()
		rows, err := dbc.Queries(ctx).ListRecentVideos(ctx)
		if err != nil {
			slog.Error("failed to fetch recent videos for SSE", "error", err)
			rows = []*db.Video{}
		}

		common.SetSSEHeaders(c)
		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		return sse.PatchElementTempl(
			templates.RecentVideosGrid(rows),
			datastar.WithSelectorID("recently-archived"),
			datastar.WithModeReplace(),
		)
	}
}
