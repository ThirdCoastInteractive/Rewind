package video_api

import (
	"fmt"
	"log/slog"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

// HandleRecent returns recently added videos
func HandleRecent(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		// Auth required
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return c.String(401, "unauthorized")
		}

		ctx := c.Request().Context()
		rows, err := dbc.Queries(ctx).ListRecentVideos(ctx)
		if err != nil {
			slog.Error("failed to fetch recent videos for SSE", "error", err)
			rows = []*db.Video{}
		}

		// Set up SSE
		common.SetSSEHeaders(c)

		sse := datastar.NewSSE(c.Response().Writer, c.Request())

		// The home page shell renders 6 skeleton slots (recent-video-slot-{0..5}).
		// Replace them one-by-one to keep each SSE message small (<~14KB) and avoid
		// a single large patch.
		for i, video := range rows {
			if sse.IsClosed() {
				return nil
			}
			slotID := fmt.Sprintf("recent-video-slot-%d", i)
			fragment := templates.RecentVideoCard(video)
			if err := sse.PatchElementTempl(fragment, datastar.WithSelectorID(slotID), datastar.WithModeReplace()); err != nil {
				slog.Error("failed to send recent video SSE patch", "error", err, "slot_id", slotID)
				return err
			}
		}

		return nil
	}
}
