// Package stitch_api provides API handlers for the clip stitch feature.
package stitch_api

import (
	"log/slog"
	"strconv"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
)

// HandleStitchSourceBrowserJSON returns the source browser contract consumed by
// the real Stitch editor.
func HandleStitchSourceBrowserJSON(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userID, _, err := common.RequireSessionUser(c, sm)
		if err != nil { return c.JSON(401, map[string]string{"error": "unauthorized"}) }
		limit := int32(30)
		if raw := c.QueryParam("limit"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 100 { limit = int32(n) }
		}
		rows, err := dbc.Queries(c.Request().Context()).SearchSourcesForStitch(c.Request().Context(), &db.SearchSourcesForStitchParams{OwnerID: userID, SourceFilter: "all", Query: c.QueryParam("q"), SortBy: "recent", Off: 0, Lim: limit})
		if err != nil { slog.Error("failed to search sources for stitch json", "error", err); return c.JSON(500, map[string]string{"error": "source search failed"}) }
		result := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			videoID := row.VideoID.String()
			clipID := ""
			if row.SourceType == "clip" { clipID = row.SourceID.String() }
			duration := row.Duration
			if duration <= 0 { duration = row.EndTs - row.StartTs }
			item := map[string]any{"id": row.SourceID.String(), "source_type": row.SourceType, "title": row.Title, "video_id": videoID, "clip_id": clipID, "source_in_us": int64(row.StartTs * 1e6), "duration_us": int64(duration * 1e6)}
			if row.SourceType == "stitch" { item["video_id"] = ""; item["export_job_id"] = row.SourceID.String() } else { item["thumbnail"] = "/api/videos/" + videoID + "/thumbnail" }
			result = append(result, item)
		}
		return c.JSON(200, map[string]any{"sources": result})
	}
}
