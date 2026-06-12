// Package stitch_api provides API handlers for the clip stitch feature.
package stitch_api

import (
	"log/slog"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates/components"
	"thirdcoast.systems/rewind/internal/db"
)

const defaultLimit = 30

// HandleStitchSourceBrowser serves the unified source browser for the stitch page.
// Sources include clips, videos, stitch exports, and compose exports.
// Query params: q (search), sort (recent|alpha|duration), source (all|clip|video|compose|stitch), offset, limit.
func HandleStitchSourceBrowser(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return c.String(401, "unauthorized")
		}

		q := c.QueryParam("q")
		sort := c.QueryParam("sort")
		if sort == "" {
			sort = "recent"
		}
		sourceFilter := c.QueryParam("source")
		if sourceFilter == "" {
			sourceFilter = "all"
		}

		offset := int32(0)
		if v := c.QueryParam("offset"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				offset = int32(n)
			}
		}
		limit := int32(defaultLimit)
		if v := c.QueryParam("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
				limit = int32(n)
			}
		}

		ctx := c.Request().Context()
		rows, err := dbc.Queries(ctx).SearchSourcesForStitch(ctx, &db.SearchSourcesForStitchParams{
			SourceFilter: sourceFilter,
			Query:        q,
			SortBy:       sort,
			Off:          offset,
			Lim:          limit + 1, // fetch one extra to know if there are more
		})
		if err != nil {
			slog.Error("failed to search sources for stitch", "error", err)
			return c.String(500, "search failed")
		}

		hasMore := len(rows) > int(limit)
		if hasMore {
			rows = rows[:limit]
		}

		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		common.SetSSEHeaders(c)
		if err := sse.PatchElementTempl(components.StitchSourceBrowserResults(rows, int(offset), hasMore)); err != nil {
			slog.Error("failed to patch source browser results", "error", err)
		}
		return nil
	}
}
