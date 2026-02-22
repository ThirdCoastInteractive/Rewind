package video_api

import (
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates/components"
	"thirdcoast.systems/rewind/internal/db"
)

// HandleCommentsRender returns an SSE-patched, server-rendered comment list.
func HandleCommentsRender(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return echo.NewHTTPError(401, "unauthorized")
		}

		videoUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		videoID := c.Param("id")

		// Read signals
		type Signals struct {
			CommentSearch string `json:"_commentSearch"`
			CommentPage   int    `json:"_commentPage"`
		}
		signals := &Signals{}
		_ = datastar.ReadSignals(c.Request(), signals)

		pageSize := 50
		page := signals.CommentPage
		if page < 0 {
			page = 0
		}

		ctx := c.Request().Context()
		q := dbc.Queries(ctx)

		// Get total count
		totalCount, err := q.CountVideoComments(ctx, videoUUID)
		if err != nil {
			totalCount = 0
		}

		var comments []components.CommentItem

		if signals.CommentSearch != "" {
			// Search mode
			rows, err := q.SearchVideoComments(ctx, &db.SearchVideoCommentsParams{
				VideoID:    videoUUID,
				Query:      signals.CommentSearch,
				PageSize:   int32(pageSize),
				PageOffset: int32(page * pageSize),
			})
			if err == nil {
				comments = searchCommentsToItems(rows)
			}
		} else {
			// Normal listing
			rows, err := q.ListVideoComments(ctx, &db.ListVideoCommentsParams{
				VideoID:    videoUUID,
				PageSize:   int32(pageSize),
				PageOffset: int32(page * pageSize),
			})
			if err == nil {
				comments = listCommentsToItems(rows)
			}
		}

		hasMore := len(comments) >= pageSize

		data := components.CommentListData{
			VideoID:     videoID,
			Comments:    comments,
			TotalCount:  totalCount,
			Page:        page,
			PageSize:    pageSize,
			HasMore:     hasMore,
			SearchQuery: signals.CommentSearch,
		}

		sse := datastar.NewSSE(c.Response().Writer, c.Request())

		if page == 0 {
			// First page: replace entire section
			_ = sse.PatchElementTempl(
				components.CommentSection(data),
				datastar.WithSelector("[data-comments-list]"),
				datastar.WithModeInner(),
			)
		} else {
			// Subsequent pages: append to existing list
			_ = sse.PatchElementTempl(
				components.CommentList(data),
				datastar.WithSelectorID("comments-list-content"),
				datastar.WithModeReplace(),
			)
		}

		return nil
	}
}

// commentFields extracts common fields from a comment row.
func toCommentItem(
	id pgtype.UUID,
	author, authorURL, text *string,
	likeCount *int64,
	publishedAt pgtype.Timestamptz,
) components.CommentItem {
	item := components.CommentItem{
		ID: id.String(),
	}
	if author != nil {
		item.Author = *author
	}
	if authorURL != nil {
		item.AuthorURL = *authorURL
	}
	if text != nil {
		item.Text = *text
	}
	if likeCount != nil {
		item.LikeCount = *likeCount
	}
	if publishedAt.Valid {
		item.PublishedAt = publishedAt.Time.Format("Jan 2, 2006")
	}
	return item
}

func listCommentsToItems(rows []*db.ListVideoCommentsRow) []components.CommentItem {
	items := make([]components.CommentItem, len(rows))
	for i, r := range rows {
		items[i] = toCommentItem(r.ID, r.Author, r.AuthorURL, r.Text, r.LikeCount, r.PublishedAt)
	}
	return items
}

func searchCommentsToItems(rows []*db.SearchVideoCommentsRow) []components.CommentItem {
	items := make([]components.CommentItem, len(rows))
	for i, r := range rows {
		items[i] = toCommentItem(r.ID, r.Author, r.AuthorURL, r.Text, r.LikeCount, r.PublishedAt)
	}
	return items
}
