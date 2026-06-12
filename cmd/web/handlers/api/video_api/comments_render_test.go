package video_api

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"thirdcoast.systems/rewind/cmd/web/templates/components"
)

func renderComp(t *testing.T, comp templ.Component) string {
	t.Helper()
	var b bytes.Buffer
	if err := comp.Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

func mustContain(t *testing.T, html string, subs ...string) {
	t.Helper()
	for _, s := range subs {
		if !strings.Contains(html, s) {
			t.Errorf("expected output to contain %q\n--- output ---\n%s", s, html)
		}
	}
}

func mustNotContain(t *testing.T, html string, subs ...string) {
	t.Helper()
	for _, s := range subs {
		if strings.Contains(html, s) {
			t.Errorf("expected output NOT to contain %q\n--- output ---\n%s", s, html)
		}
	}
}

// CommentRow must turn inline timestamps into seek buttons.
func TestCommentRow_TimestampSeek(t *testing.T) {
	html := renderComp(t, components.CommentRow(components.CommentItem{
		Author: "alice", Text: "the good part starts at 1:23 trust me",
	}))
	mustContain(t, html, "window.seekToTime(83", "1:23", "the good part starts at ")
}

// CommentRow must render the pre-built highlight HTML raw (not escaped), and
// NOT also render the plain Text.
func TestCommentRow_HighlightRaw(t *testing.T) {
	html := renderComp(t, components.CommentRow(components.CommentItem{
		Author: "bob", Text: "PLAINTEXT", Highlighted: "a <mark>hit</mark> here",
	}))
	mustContain(t, html, "<mark>hit</mark>")
	mustNotContain(t, html, "PLAINTEXT")
}

// CommentRow must surface avatar + badges from the raw fields.
func TestCommentRow_Badges(t *testing.T) {
	html := renderComp(t, components.CommentRow(components.CommentItem{
		Author:      "creator",
		AuthorThumb: "https://yt3.example/avatar.jpg",
		LikeCount:   1500,
		TimeLabel:   "2 years ago",
		IsUploader:  true,
		IsVerified:  true,
		IsPinned:    true,
		IsFavorited: true,
	}))
	mustContain(t, html,
		"https://yt3.example/avatar.jpg", // avatar
		"Creator",                        // uploader badge
		"fa-circle-check",                // verified
		"Pinned",                         // pinned
		"fa-heart",                       // creator-hearted
		"2 years ago",                    // relative time
		"1.5K",                           // compact like count
	)
}

// CommentRow must render a reply expander + container only when ReplyCount > 0.
func TestCommentRow_Replies(t *testing.T) {
	with := renderComp(t, components.CommentRow(components.CommentItem{
		Author: "x", VideoID: "VID", CommentID: "CID123", ReplyCount: 3,
	}))
	mustContain(t, with,
		"mode=replies",
		"parent=CID123", // note: rendered as &amp;parent=... in the attribute
		`id="replies-CID123"`,
		"View 3 replies",
	)
	without := renderComp(t, components.CommentRow(components.CommentItem{
		Author: "y", VideoID: "VID", CommentID: "CID999", ReplyCount: 0,
	}))
	mustNotContain(t, without, "replies-CID999", "mode=replies")
}

// LOAD MORE must target mode=page (append) and advance the page signal.
func TestCommentLoadMore(t *testing.T) {
	more := renderComp(t, components.CommentLoadMore(components.CommentListData{
		VideoID: "VID", Page: 1, HasMore: true,
	}))
	mustContain(t, more, "LOAD MORE", "mode=page", "$_commentPage = 2")

	none := renderComp(t, components.CommentLoadMore(components.CommentListData{
		VideoID: "VID", Page: 1, HasMore: false,
	}))
	mustNotContain(t, none, "LOAD MORE")
}

// The section's search input must reset the page and target mode=search, and
// the stable containers must exist for SSE to target.
func TestCommentSection_SearchWiring(t *testing.T) {
	html := renderComp(t, components.CommentSection(components.CommentListData{
		VideoID: "VID", TotalCount: 42,
	}))
	mustContain(t, html,
		`data-bind="_commentSearch"`,
		"mode=search",
		"$_commentPage = 0",
		`id="comments-list-content"`,
		`id="comments-load-more"`,
		"42 COMMENTS",
	)
}

// Empty states differ for "no results for query" vs "no comments", and only on
// page 0.
func TestCommentRows_EmptyStates(t *testing.T) {
	q := renderComp(t, components.CommentRows(components.CommentListData{
		Page: 0, SearchQuery: "needle",
	}))
	mustContain(t, q, `No comments matching "needle"`)

	none := renderComp(t, components.CommentRows(components.CommentListData{Page: 0}))
	mustContain(t, none, "No comments yet.")

	// page > 0 must never inject an empty state (append pagination).
	appended := renderComp(t, components.CommentRows(components.CommentListData{Page: 2}))
	mustNotContain(t, appended, "No comments")
}
