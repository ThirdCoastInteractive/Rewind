// package video_api provides video-related API handlers.
package video_api

import (
	"log/slog"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/cmd/web/templates/components"
	"thirdcoast.systems/rewind/internal/db"
)

// HandleIndex returns a filtered/paginated list of videos.
func HandleIndex(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, _, err := sm.GetSession(c.Request())
		if err != nil {
			return c.String(401, "unauthorized")
		}

		// Parse parameters from DataStar signals (with query param fallback)
		type Signals struct {
			Query      string   `json:"q"`
			Sort       string   `json:"sort"`
			Duration   string   `json:"duration"`
			Uploader   string   `json:"uploader"`
			Tags       []string `json:"tags"`
			DateType   *string  `json:"dateType"`
			DateFrom   *string  `json:"dateFrom"`
			DateTo     *string  `json:"dateTo"`
			HasClips   bool     `json:"hasClips"`
			HasMarkers bool     `json:"hasMarkers"`
			Page       int      `json:"page"`
			PageSize   int      `json:"pageSize"`
		}
		signals := &Signals{}
		if err := datastar.ReadSignals(c.Request(), signals); err != nil {
			// Fallback to query params for initial load
			signals.Query = strings.TrimSpace(c.QueryParam("q"))
			signals.Sort = c.QueryParam("sort")
			signals.Duration = c.QueryParam("duration")
			signals.Uploader = c.QueryParam("uploader")
			signals.Tags = parseTagsString(c.QueryParam("tags"))
			if dt := c.QueryParam("dateType"); dt != "" {
				signals.DateType = &dt
			}
			if df := c.QueryParam("dateFrom"); df != "" {
				signals.DateFrom = &df
			}
			if dto := c.QueryParam("dateTo"); dto != "" {
				signals.DateTo = &dto
			}
			signals.HasClips = c.QueryParam("hasClips") == "true"
			signals.HasMarkers = c.QueryParam("hasMarkers") == "true"
			if p, err := strconv.Atoi(c.QueryParam("page")); err == nil {
				signals.Page = p
			}
			if ps, err := strconv.Atoi(c.QueryParam("pageSize")); err == nil {
				signals.PageSize = ps
			}
		}

		// Build validated params
		params := DefaultVideosListParams()
		params.Query = strings.TrimSpace(signals.Query)
		if signals.Sort != "" {
			params.Sort = signals.Sort
		}
		params.Duration = signals.Duration
		params.Uploader = signals.Uploader
		if len(signals.Tags) > 0 {
			params.Tags = signals.Tags
		}
		if signals.DateType != nil {
			params.DateType = *signals.DateType
		}
		if signals.DateFrom != nil {
			params.DateFrom = *signals.DateFrom
		}
		if signals.DateTo != nil {
			params.DateTo = *signals.DateTo
		}
		params.HasClips = signals.HasClips
		params.HasMarkers = signals.HasMarkers
		if signals.Page > 0 {
			params.Page = signals.Page
		}
		if signals.PageSize > 0 {
			params.PageSize = signals.PageSize
		}
		params.Validate()

		// Query database
		ctx := c.Request().Context()
		dbParams := &db.ListVideosPaginatedParams{
			Query:          nullableString(params.Query),
			Uploader:       nullableString(params.Uploader),
			ChannelID:      nil,
			DurationFilter: nullableString(params.Duration),
			Tags:           params.Tags,
			DateType:       nullableString(params.DateType),
			DateFrom:       parseDate(params.DateFrom),
			DateTo:         parseDate(params.DateTo),
			HasClips:       nullableBool(params.HasClips),
			HasMarkers:     nullableBool(params.HasMarkers),
			SortOrder:      params.Sort,
			PageOffset:     params.Offset(),
			PageLimit:      int32(params.PageSize),
		}

		rows, err := dbc.Queries(ctx).ListVideosPaginated(ctx, dbParams)
		if err != nil {
			slog.Error("failed to fetch videos", "error", err)
			rows = []*db.ListVideosPaginatedRow{}
		}

		// Build pagination info
		var totalCount int64
		if len(rows) > 0 {
			totalCount = rows[0].TotalCount
		}
		totalPages := int((totalCount + int64(params.PageSize) - 1) / int64(params.PageSize))
		if totalPages < 1 {
			totalPages = 1
		}

		pagination := components.Pagination{
			CurrentPage: params.Page,
			TotalPages:  totalPages,
			TotalItems:  int(totalCount),
			PageSize:    params.PageSize,
			HasPrev:     params.Page > 1,
			HasNext:     params.Page < totalPages,
		}

		// Set up SSE
		common.SetSSEHeaders(c)

		sse := datastar.NewSSE(c.Response().Writer, c.Request())

		// Patch the grid
		if err := sse.PatchElementTempl(templates.VideosGrid(rows)); err != nil {
			slog.Error("failed to send videos grid SSE patch", "error", err)
			return err
		}

		// Patch the pagination controls
		if err := sse.PatchElementTempl(components.PaginationControls(pagination)); err != nil {
			slog.Error("failed to send pagination SSE patch", "error", err)
			return err
		}

		return nil
	}
}
