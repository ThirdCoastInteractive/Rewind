package content

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/topics"
	"thirdcoast.systems/rewind/internal/wiki"
)

func wikiStore(dbc *db.DatabaseConnection) *wiki.Store {
	return wiki.New(dbc)
}

// HandleWikiIndex serves GET /wiki — trees, recent pages, search box.
func HandleWikiIndex(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, username, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(http.StatusFound, "/login")
		}
		ctx := c.Request().Context()
		store := wiki.NewForContext(dbc, ctx)
		q := strings.TrimSpace(c.QueryParam("q"))
		var recent []wiki.Page
		var hits []wiki.SearchHit
		if q != "" {
			hits, _ = store.Search(ctx, q, "", 40)
		} else {
			recent, _ = store.ListRecent(ctx, 30)
		}
		trees := make([]templates.WikiTreeSummary, 0, len(wiki.Trees))
		for _, t := range wiki.Trees {
			pages, _ := store.ListByTree(ctx, t)
			samples := make([]string, 0, 3)
			for _, p := range pages {
				if len(samples) >= 3 {
					break
				}
				if title := strings.TrimSpace(p.Title); title != "" {
					samples = append(samples, title)
				}
			}
			trees = append(trees, templates.WikiTreeSummary{
				Tree:    t,
				Label:   wiki.TreeLabel(t),
				Count:   len(pages),
				Blurb:   wiki.TreeBlurb(t),
				Samples: samples,
			})
		}
		return templates.WikiIndex(username, trees, recent, hits, q).Render(ctx, c.Response())
	}
}

// HandleWikiPage serves GET /wiki/:tree/* for show, edit, and history.
// Nested slugs use Echo's * catch-all (e.g. ben-avery/2026-09-03/edit).
func HandleWikiPage(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, username, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(http.StatusFound, "/login")
		}
		store := wiki.NewForContext(dbc, c.Request().Context())
		tree := c.Param("tree")
		if !wiki.ValidTree(tree) {
			return echo.NewHTTPError(http.StatusNotFound, "unknown wiki tree")
		}
		slug, action := splitWikiRest(c.Param("*"))
		slug = wiki.NormalizeSlug(slug)
		if action == "show" && (slug == "" || slug == "index") {
			if slug == "index" {
				if page, err := store.Get(c.Request().Context(), tree, "index"); err == nil {
					return renderWikiShow(c, store, dbc, username, page)
				}
			}
			return renderWikiTree(c, store, username, tree)
		}
		if slug == "" {
			return c.Redirect(http.StatusFound, "/wiki")
		}
		switch action {
		case "edit":
			return renderWikiEdit(c, store, username, tree, slug, "", 0)
		case "history":
			return renderWikiHistory(c, store, username, tree, slug)
		default:
			return renderWikiShowBySlug(c, store, dbc, username, tree, slug)
		}
	}
}

// HandleWikiSave serves POST /wiki/save — put page with revision check.
func HandleWikiSave(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userID, username, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(http.StatusFound, "/login")
		}
		ctx := c.Request().Context()
		store := wiki.NewForContext(dbc, ctx)
		tree := strings.TrimSpace(c.FormValue("tree"))
		slug := wiki.NormalizeSlug(c.FormValue("slug"))
		title := strings.TrimSpace(c.FormValue("title"))
		body := c.FormValue("body")
		summary := strings.TrimSpace(c.FormValue("summary"))
		expected, _ := strconv.Atoi(strings.TrimSpace(c.FormValue("expected_revision")))
		if !wiki.ValidTree(tree) || slug == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "tree and slug required")
		}
		if title == "" || summary == "" {
			return renderWikiEdit(c, store, username, tree, slug, "Title and summary are required.", expected)
		}
		page, err := store.Put(ctx, wiki.PutInput{
			Tree:             tree,
			Slug:             slug,
			Title:            title,
			Body:             body,
			Summary:          summary,
			ExpectedRevision: int32(expected),
			Actor: wiki.Actor{
				Kind:       "user",
				ID:         "user:" + userID.String(),
				SessionID:  "ui",
				ClientName: "web",
				UserID:     userID,
			},
		})
		if err != nil {
			if errors.Is(err, wiki.ErrConflict) {
				cur, _ := store.Get(ctx, tree, slug)
				msg := "Revision conflict — reload and retry. Current revision: " + strconv.Itoa(int(cur.Revision))
				return renderWikiEdit(c, store, username, tree, slug, msg, int(cur.Revision))
			}
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		return c.Redirect(http.StatusSeeOther, wiki.PageURL(page.Tree, page.Slug))
	}
}

func renderWikiTree(c echo.Context, store *wiki.Store, username, tree string) error {
	ctx := c.Request().Context()
	pages, err := store.ListByTree(ctx, tree)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return templates.WikiTreeIndex(username, tree, pages).Render(ctx, c.Response())
}

func renderWikiShowBySlug(c echo.Context, store *wiki.Store, dbc *db.DatabaseConnection, username, tree, slug string) error {
	ctx := c.Request().Context()
	page, err := store.Get(ctx, tree, slug)
	if err != nil {
		if errors.Is(err, wiki.ErrNotFound) {
			return templates.WikiMissing(username, tree, slug).Render(ctx, c.Response())
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return renderWikiShow(c, store, dbc, username, page)
}

func renderWikiShow(c echo.Context, store *wiki.Store, dbc *db.DatabaseConnection, username string, page wiki.Page) error {
	ctx := c.Request().Context()
	page = wiki.DecorateMedia(page, wikiClipLookup(ctx, dbc))
	view := templates.WikiView{
		Page: page,
		TOC:  wiki.ArticleHeadings(page.HTML),
	}
	if parentSlug := wiki.ParentSlug(page.Slug); parentSlug != "" {
		if parent, err := store.Get(ctx, page.Tree, parentSlug); err == nil {
			view.Parent = wiki.Ref{Tree: parent.Tree, Slug: parent.Slug, Title: parent.Title}
		} else {
			view.Parent = wiki.Ref{Tree: page.Tree, Slug: parentSlug, Title: parentSlug}
		}
	}
	if all, err := store.ListByTree(ctx, page.Tree); err == nil {
		prefix := page.Slug + "/"
		for _, p := range all {
			if strings.HasPrefix(p.Slug, prefix) {
				view.Subpages = append(view.Subpages, p)
			}
		}
	}
	if dbc != nil {
		q := dbc.Queries(ctx)
		if page.CreatorID.Valid {
			if cr, err := q.GetCreator(ctx, page.CreatorID); err == nil && cr != nil {
				view.CreatorName = cr.Name
			}
		}
		if page.ChannelID.Valid {
			if ch, err := q.GetChannel(ctx, page.ChannelID); err == nil && ch != nil {
				view.ChannelName = ch.Uploader
				if view.ChannelName == "" {
					view.ChannelName = ch.IdentityKey
				}
				view.ChannelPath = ch.Uploader
				if view.ChannelPath == "" {
					view.ChannelPath = ch.IdentityKey
				}
			}
		}
	}
	if page.Tree == wiki.TreeTopic {
		arch, err := topics.New(dbc).LoadArchive(ctx, page.Slug, 40)
		if err == nil {
			view.ArchiveWindowCount = int(arch.WindowCount)
			view.ArchiveChannelCount = int(arch.ChannelCount)
			for _, w := range arch.Windows {
				view.ArchiveWindows = append(view.ArchiveWindows, templates.WikiArchiveWindow{
					ID: w.ID, VideoID: w.VideoID, Title: w.Title, VideoTitle: w.VideoTitle,
					Uploader: w.Uploader, Start: w.Start, End: w.End, WebPath: w.WebPath,
					CutPath: "/videos/" + w.VideoID + "/cut?t=" + strconv.FormatFloat(w.Start, 'f', 3, 64),
				})
			}
			for _, t := range arch.Related {
				view.RelatedTopics = append(view.RelatedTopics, templates.WikiRelatedTopic{Slug: t.Slug, Title: t.Title, Count: t.Count})
			}
		}
	}
	return templates.WikiShow(username, view).Render(ctx, c.Response())
}

func renderWikiEdit(c echo.Context, store *wiki.Store, username, tree, slug, formErr string, expected int) error {
	ctx := c.Request().Context()
	page, err := store.Get(ctx, tree, slug)
	if err != nil {
		if !errors.Is(err, wiki.ErrNotFound) {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		page = wiki.Page{Tree: tree, Slug: slug, Title: slug, Revision: 0}
	}
	// Prefer posted fields so a failed save keeps the user's draft.
	if c.Request().Method == http.MethodPost {
		if v := strings.TrimSpace(c.FormValue("title")); v != "" {
			page.Title = v
		}
		page.Body = c.FormValue("body")
		page.Summary = strings.TrimSpace(c.FormValue("summary"))
	}
	if expected > 0 {
		page.Revision = int32(expected)
	}
	return templates.WikiEdit(username, page, formErr).Render(ctx, c.Response())
}

func renderWikiHistory(c echo.Context, store *wiki.Store, username, tree, slug string) error {
	ctx := c.Request().Context()
	page, err := store.Get(ctx, tree, slug)
	if err != nil {
		if errors.Is(err, wiki.ErrNotFound) {
			return templates.WikiMissing(username, tree, slug).Render(ctx, c.Response())
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	revs, err := store.History(ctx, tree, slug)
	if err != nil && !errors.Is(err, wiki.ErrNotFound) {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return templates.WikiHistory(username, page, revs).Render(ctx, c.Response())
}

// splitWikiRest peels /edit or /history off a catch-all path.
func splitWikiRest(rest string) (slug, action string) {
	rest = strings.Trim(rest, "/")
	if rest == "" {
		return "", "show"
	}
	for _, act := range []string{"edit", "history"} {
		if rest == act {
			return "", act
		}
		suf := "/" + act
		if strings.HasSuffix(rest, suf) {
			return strings.TrimSuffix(rest, suf), act
		}
	}
	return rest, "show"
}

func wikiClipLookup(ctx context.Context, dbc *db.DatabaseConnection) wiki.ClipLookup {
	if dbc == nil {
		return nil
	}
	q := dbc.Queries(ctx)
	return func(id string) (wiki.ClipRef, bool) {
		var pid pgtype.UUID
		if err := pid.Scan(id); err != nil || !pid.Valid {
			return wiki.ClipRef{}, false
		}
		clip, err := q.GetClip(ctx, pid)
		if err != nil || clip == nil {
			return wiki.ClipRef{}, false
		}
		return wiki.ClipRef{
			VideoID: clip.VideoID.String(),
			Title:   clip.Title,
			Start:   clip.StartTs,
			End:     clip.EndTs,
		}, true
	}
}

func decorateWikiPages(pages []wiki.Page, lookup wiki.ClipLookup) []wiki.Page {
	if len(pages) == 0 {
		return pages
	}
	out := make([]wiki.Page, len(pages))
	for i, p := range pages {
		out[i] = wiki.DecorateMedia(p, lookup)
	}
	return out
}
