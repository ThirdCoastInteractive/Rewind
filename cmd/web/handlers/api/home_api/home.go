package home_api

import (
	"log/slog"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)
// HandleStats serves GET /api/home/stats, returning aggregate dashboard statistics via SSE.
func HandleStats(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return c.String(401, "unauthorized")
		}

		ctx := c.Request().Context()
		stats, err := dbc.Queries(ctx).GetHomeStats(ctx)
		if err != nil {
			slog.Error("failed to fetch home stats", "error", err)
			stats = &db.GetHomeStatsRow{}
		}

		common.SetSSEHeaders(c)
		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		return sse.PatchElementTempl(
			templates.HomeStatsRow(stats),
			datastar.WithSelectorID("home-stats"),
			datastar.WithModeReplace(),
		)
	}
}
// HandleRecentPublished serves GET /api/home/recent-published, returning recently archived videos via SSE.
func HandleRecentPublished(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return c.String(401, "unauthorized")
		}

		ctx := c.Request().Context()
		videos, err := dbc.Queries(ctx).ListRecentlyPublishedVideos(ctx)
		if err != nil {
			slog.Error("failed to fetch recently published videos", "error", err)
			videos = []*db.Video{}
		}

		common.SetSSEHeaders(c)
		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		return sse.PatchElementTempl(
			templates.HomeVideoSection("recently-published", videos),
			datastar.WithSelectorID("recently-published"),
			datastar.WithModeReplace(),
		)
	}
}
// HandleRecentClips serves GET /api/home/recent-clips, returning recently created clips via SSE.
func HandleRecentClips(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return c.String(401, "unauthorized")
		}

		ctx := c.Request().Context()
		clips, err := dbc.Queries(ctx).ListRecentClips(ctx)
		if err != nil {
			slog.Error("failed to fetch recent clips", "error", err)
			clips = []*db.ListRecentClipsRow{}
		}

		common.SetSSEHeaders(c)
		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		return sse.PatchElementTempl(
			templates.HomeClipsSection(clips),
			datastar.WithSelectorID("recent-clips"),
			datastar.WithModeReplace(),
		)
	}
}
