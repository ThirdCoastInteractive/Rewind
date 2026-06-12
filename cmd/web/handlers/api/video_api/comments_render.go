package video_api

import (
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates/components"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/utils/commentfmt"
)

const commentsPageSize = 50

// HandleCommentsRender returns an SSE-patched, server-rendered comment list.
//
// The ?mode query param selects what gets patched, which keeps the search
// input from being re-rendered (and losing focus) and makes LOAD MORE append
// instead of replace:
//   - ""       initial load: render the whole CommentSection (input + list).
//   - "search" replace just the rows (input untouched); page is reset to 0.
//   - "page"   append the next page's rows to the existing list.
//   - "replies" load a comment's replies into its inline container.
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
		mode := c.QueryParam("mode")

		ctx := c.Request().Context()
		q := dbc.Queries(ctx)
		sse := datastar.NewSSE(c.Response().Writer, c.Request())

		// Replies: load a single parent comment's children into its container.
		if mode == "replies" {
			parent := c.QueryParam("parent")
			if parent == "" {
				return nil
			}
			rows, err := q.ListVideoCommentReplies(ctx, &db.ListVideoCommentRepliesParams{
				VideoID:         videoUUID,
				ParentCommentID: parent,
			})
			var replies []components.CommentItem
			if err == nil {
				replies = replyCommentsToItems(videoID, rows)
			}
			_ = sse.PatchElementTempl(
				components.CommentReplies(replies),
				datastar.WithSelectorID("replies-"+parent),
				datastar.WithModeInner(),
			)
			return nil
		}

		// Read signals
		type Signals struct {
			CommentSearch string `json:"_commentSearch"`
			CommentPage   int    `json:"_commentPage"`
		}
		signals := &Signals{}
		_ = datastar.ReadSignals(c.Request(), signals)

		page := signals.CommentPage
		if page < 0 {
			page = 0
		}
		search := strings.TrimSpace(signals.CommentSearch)

		totalCount, err := q.CountVideoComments(ctx, videoUUID)
		if err != nil {
			totalCount = 0
		}

		var comments []components.CommentItem
		if search != "" {
			rows, err := q.SearchVideoComments(ctx, &db.SearchVideoCommentsParams{
				VideoID:    videoUUID,
				Query:      search,
				PageSize:   int32(commentsPageSize),
				PageOffset: int32(page * commentsPageSize),
			})
			if err == nil {
				comments = searchCommentsToItems(videoID, rows)
			}
		} else {
			rows, err := q.ListVideoComments(ctx, &db.ListVideoCommentsParams{
				VideoID:    videoUUID,
				PageSize:   int32(commentsPageSize),
				PageOffset: int32(page * commentsPageSize),
			})
			if err == nil {
				comments = listCommentsToItems(videoID, rows)
			}
		}

		data := components.CommentListData{
			VideoID:     videoID,
			Comments:    comments,
			TotalCount:  totalCount,
			Page:        page,
			PageSize:    commentsPageSize,
			HasMore:     len(comments) >= commentsPageSize,
			SearchQuery: search,
		}

		switch mode {
		case "page":
			// Append the new page's rows; refresh the load-more slot.
			_ = sse.PatchElementTempl(components.CommentRows(data),
				datastar.WithSelectorID("comments-list-content"), datastar.WithModeAppend())
			_ = sse.PatchElementTempl(components.CommentLoadMore(data),
				datastar.WithSelectorID("comments-load-more"), datastar.WithModeInner())
		case "search":
			// Replace just the rows (the search input keeps focus); refresh load-more.
			_ = sse.PatchElementTempl(components.CommentRows(data),
				datastar.WithSelectorID("comments-list-content"), datastar.WithModeInner())
			_ = sse.PatchElementTempl(components.CommentLoadMore(data),
				datastar.WithSelectorID("comments-load-more"), datastar.WithModeInner())
		default:
			// Initial load: render the whole section.
			_ = sse.PatchElementTempl(components.CommentSection(data),
				datastar.WithSelector("[data-comments-list]"), datastar.WithModeInner())
		}
		return nil
	}
}

func deref(s *string) string {
	if s != nil {
		return *s
	}
	return ""
}

func derefInt64(p *int64) int64 {
	if p != nil {
		return *p
	}
	return 0
}

// commentTimeLabel prefers yt-dlp's relative "_time_text" ("2 years ago"),
// falling back to the absolute published date.
func commentTimeLabel(timeText string, publishedAt pgtype.Timestamptz) string {
	if strings.TrimSpace(timeText) != "" {
		return timeText
	}
	if publishedAt.Valid {
		return publishedAt.Time.Format("Jan 2, 2006")
	}
	return ""
}

func listCommentsToItems(videoID string, rows []*db.ListVideoCommentsRow) []components.CommentItem {
	items := make([]components.CommentItem, len(rows))
	for i, r := range rows {
		items[i] = components.CommentItem{
			ID:          r.ID.String(),
			VideoID:     videoID,
			CommentID:   r.CommentID,
			Author:      deref(r.Author),
			AuthorURL:   deref(r.AuthorURL),
			AuthorThumb: r.AuthorThumbnail,
			Text:        deref(r.Text),
			LikeCount:   derefInt64(r.LikeCount),
			TimeLabel:   commentTimeLabel(r.TimeText, r.PublishedAt),
			ReplyCount:  r.ReplyCount,
			IsFavorited: r.IsFavorited,
			IsPinned:    r.IsPinned,
			IsUploader:  r.AuthorIsUploader,
			IsVerified:  r.AuthorIsVerified,
		}
	}
	return items
}

func searchCommentsToItems(videoID string, rows []*db.SearchVideoCommentsRow) []components.CommentItem {
	items := make([]components.CommentItem, len(rows))
	for i, r := range rows {
		items[i] = components.CommentItem{
			ID:          r.ID.String(),
			VideoID:     videoID,
			CommentID:   r.CommentID,
			Author:      deref(r.Author),
			AuthorURL:   deref(r.AuthorURL),
			AuthorThumb: r.AuthorThumbnail,
			Text:        deref(r.Text),
			Highlighted: commentfmt.SafeHighlight(r.Highlighted),
			LikeCount:   derefInt64(r.LikeCount),
			TimeLabel:   commentTimeLabel(r.TimeText, r.PublishedAt),
			ReplyCount:  r.ReplyCount,
			IsFavorited: r.IsFavorited,
			IsPinned:    r.IsPinned,
			IsUploader:  r.AuthorIsUploader,
			IsVerified:  r.AuthorIsVerified,
		}
	}
	return items
}

func replyCommentsToItems(videoID string, rows []*db.ListVideoCommentRepliesRow) []components.CommentItem {
	items := make([]components.CommentItem, len(rows))
	for i, r := range rows {
		items[i] = components.CommentItem{
			ID:          r.ID.String(),
			VideoID:     videoID,
			CommentID:   r.CommentID,
			Author:      deref(r.Author),
			AuthorURL:   deref(r.AuthorURL),
			AuthorThumb: r.AuthorThumbnail,
			Text:        deref(r.Text),
			LikeCount:   derefInt64(r.LikeCount),
			TimeLabel:   commentTimeLabel(r.TimeText, r.PublishedAt),
			IsFavorited: r.IsFavorited,
			IsPinned:    r.IsPinned,
			IsUploader:  r.AuthorIsUploader,
			IsVerified:  r.AuthorIsVerified,
		}
	}
	return items
}
