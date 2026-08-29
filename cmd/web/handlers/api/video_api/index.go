// package video_api provides video-related API handlers.
package video_api

import (
	"log/slog"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/cmd/web/templates/components"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/search"
)

// videosListSignals is the DataStar/query shape for the library grid.
type videosListSignals struct {
	Query      string   `json:"q"`
	Sort       string   `json:"sort"`
	Duration   string   `json:"duration"`
	Uploader   string   `json:"uploader"`
	Tags       []string `json:"tags"`
	TagIDs     []string `json:"tagIds"`
	DateType   *string  `json:"dateType"`
	DateFrom   *string  `json:"dateFrom"`
	DateTo     *string  `json:"dateTo"`
	HasClips   bool     `json:"hasClips"`
	HasMarkers bool     `json:"hasMarkers"`
	Page       int      `json:"page"`
	PageSize   int      `json:"pageSize"`
}

// HandleIndex returns a filtered/paginated list of videos.
func HandleIndex(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, _, err := sm.GetSession(c.Request())
		if err != nil {
			return c.String(401, "unauthorized")
		}

		signals := &videosListSignals{}
		if err := datastar.ReadSignals(c.Request(), signals); err != nil {
			// Fallback to query params for initial load
			signals.Query = strings.TrimSpace(c.QueryParam("q"))
			signals.Sort = c.QueryParam("sort")
			signals.Duration = c.QueryParam("duration")
			signals.Uploader = c.QueryParam("uploader")
			signals.Tags = parseTagsString(c.QueryParam("tags"))
			signals.TagIDs = parseTagsString(c.QueryParam("tagIds"))
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

		common.SetSSEHeaders(c)
		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		return patchVideosList(c, dbc, sse, signals)
	}
}

// patchVideosList queries the library with the given signals and patches grid + pagination.
func patchVideosList(c echo.Context, dbc *db.DatabaseConnection, sse *datastar.ServerSentEventGenerator, signals *videosListSignals) error {
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
	compiled := search.Compile(params.Query)
	if compiled.Raw != "" && params.Sort == "newest" {
		// Default listing sort is newest; switch to relevance while a query is
		// active unless the user picked something else.
		params.Sort = "relevance"
	}

	// If the current page is now empty after deletions, clamp to last page on a second fetch.
	ctx := c.Request().Context()
	fetch := func(page int) ([]*db.ListVideosPaginatedRow, components.Pagination) {
		p := params
		p.Page = page
		p.Validate()
		dbParams := &db.ListVideosPaginatedParams{
			Query:          nullableString(compiled.Raw),
			Tsquery:        nullableString(compiled.TSQuery),
			Uploader:       nullableString(p.Uploader),
			ChannelID:      nil,
			DurationFilter: nullableString(p.Duration),
			Tags:           p.Tags,
			TagIds:         parseUUIDList(signals.TagIDs),
			DateType:       nullableString(p.DateType),
			DateFrom:       parseDate(p.DateFrom),
			DateTo:         parseDate(p.DateTo),
			HasClips:       nullableBool(p.HasClips),
			HasMarkers:     nullableBool(p.HasMarkers),
			SortOrder:      p.Sort,
			PageOffset:     p.Offset(),
			PageLimit:      int32(p.PageSize),
		}
		rows, err := dbc.Queries(ctx).ListVideosPaginated(ctx, dbParams)
		if err != nil {
			slog.Error("failed to fetch videos", "error", err)
			rows = []*db.ListVideosPaginatedRow{}
		}
		var totalCount int64
		if len(rows) > 0 {
			totalCount = rows[0].TotalCount
		}
		totalPages := int((totalCount + int64(p.PageSize) - 1) / int64(p.PageSize))
		if totalPages < 1 {
			totalPages = 1
		}
		if p.Page > totalPages {
			p.Page = totalPages
		}
		pagination := components.Pagination{
			CurrentPage: p.Page,
			TotalPages:  totalPages,
			TotalItems:  int(totalCount),
			PageSize:    p.PageSize,
			HasPrev:     p.Page > 1,
			HasNext:     p.Page < totalPages,
		}
		return rows, pagination
	}

	page := params.Page
	rows, pagination := fetch(page)
	// After bulk deletes (or a stale page query) the page may be empty while
	// earlier pages still have results — walk back until we find content.
	for len(rows) == 0 && page > 1 {
		page--
		rows, pagination = fetch(page)
	}

	if err := sse.PatchElementTempl(templates.VideosGrid(rows)); err != nil {
		slog.Error("failed to send videos grid SSE patch", "error", err)
		return err
	}
	if err := sse.PatchElementTempl(components.PaginationControls(pagination)); err != nil {
		slog.Error("failed to send pagination SSE patch", "error", err)
		return err
	}
	// Keep page signal in sync when we clamped after deletes.
	if pagination.CurrentPage != params.Page {
		_ = sse.PatchSignals([]byte(`{"page":` + strconv.Itoa(pagination.CurrentPage) + `}`))
	}
	return nil
}

// parseUUIDList parses tag id strings into a UUID slice for the tag filter,
// returning nil (→ NULL filter, i.e. no tag filtering) when none are valid.
func parseUUIDList(ids []string) []pgtype.UUID {
	out := make([]pgtype.UUID, 0, len(ids))
	for _, s := range ids {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if g, err := uuid.Parse(s); err == nil {
			out = append(out, pgtype.UUID{Bytes: [16]byte(g), Valid: true})
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
