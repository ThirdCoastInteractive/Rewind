package wiki

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
)

// ErrRevisionConflict is returned when ExpectedRevision does not match the tip.
var ErrRevisionConflict = errors.New("wiki revision conflict")

// Store persists wiki pages against Postgres.
type Store struct {
	db *db.DatabaseConnection
}

// New returns a Store bound to dbc.
func New(dbc *db.DatabaseConnection) *Store {
	return &Store{db: dbc}
}

// Put creates or updates a page inside a transaction. Missing page + ExpectedRevision=0
// inserts revision 1. Existing pages require ExpectedRevision == current tip.
func (s *Store) Put(ctx context.Context, in PutInput) (Page, error) {
	if !ValidTree(in.Tree) {
		return Page{}, fmt.Errorf("unknown tree %q", in.Tree)
	}
	slug := Slug(in.Slug)
	if slug == "" {
		return Page{}, fmt.Errorf("slug required")
	}
	if strings.TrimSpace(in.Summary) == "" {
		return Page{}, fmt.Errorf("summary required")
	}
	if strings.TrimSpace(in.Title) == "" {
		return Page{}, fmt.Errorf("title required")
	}
	if !in.Actor.valid() {
		return Page{}, fmt.Errorf("invalid actor kind %q", in.Actor.Kind)
	}

	q, tx, err := s.db.NewWithTX(ctx)
	if err != nil {
		return Page{}, err
	}
	defer tx.Rollback(ctx)

	existing, err := q.GetWikiPage(ctx, &db.GetWikiPageParams{Tree: in.Tree, Slug: slug})
	missing := errors.Is(err, pgx.ErrNoRows)
	if err != nil && !missing {
		return Page{}, err
	}

	var (
		page     *db.WikiPage
		prevBody string
	)
	if missing {
		if in.ExpectedRevision != 0 {
			return Page{}, fmt.Errorf("%w: page does not exist", ErrRevisionConflict)
		}
		page, err = q.InsertWikiPage(ctx, &db.InsertWikiPageParams{
			Tree:      in.Tree,
			Slug:      slug,
			Title:     in.Title,
			Body:      in.Body,
			CreatorID: in.CreatorID,
			ChannelID: in.ChannelID,
			UpdatedBy: in.Actor.updatedBy(),
		})
		if err != nil {
			if db.IsUniqueViolationErr(err) {
				return Page{}, fmt.Errorf("%w: page was created concurrently", ErrRevisionConflict)
			}
			return Page{}, err
		}
	} else {
		if in.ExpectedRevision != existing.Revision {
			return Page{}, fmt.Errorf("%w: current revision is %d", ErrRevisionConflict, existing.Revision)
		}
		prevBody = existing.Body
		page, err = q.UpdateWikiPage(ctx, &db.UpdateWikiPageParams{
			Title:            in.Title,
			Body:             in.Body,
			CreatorID:        in.CreatorID,
			ChannelID:        in.ChannelID,
			UpdatedBy:        in.Actor.updatedBy(),
			Tree:             in.Tree,
			Slug:             slug,
			ExpectedRevision: in.ExpectedRevision,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return Page{}, fmt.Errorf("%w: current revision changed", ErrRevisionConflict)
			}
			return Page{}, err
		}
	}

	_, err = q.InsertWikiRevision(ctx, &db.InsertWikiRevisionParams{
		Tree:          page.Tree,
		Slug:          page.Slug,
		Revision:      page.Revision,
		Title:         page.Title,
		Body:          page.Body,
		Diff:          UnifiedDiff(prevBody, page.Body),
		Summary:       strings.TrimSpace(in.Summary),
		ActorKind:     in.Actor.Kind,
		ActorID:       in.Actor.ID,
		UserID:        in.Actor.UserID,
		SessionID:     in.Actor.SessionID,
		ClientName:    in.Actor.ClientName,
		ClientVersion: in.Actor.ClientVersion,
		TokenName:     in.Actor.TokenName,
	})
	if err != nil {
		return Page{}, err
	}

	if err := q.DeleteWikiLinksForPage(ctx, &db.DeleteWikiLinksForPageParams{Tree: page.Tree, Slug: page.Slug}); err != nil {
		return Page{}, err
	}
	for _, link := range ParseLinks(page.Body, page.Tree) {
		if err := q.InsertWikiLink(ctx, &db.InsertWikiLinkParams{
			FromTree: page.Tree,
			FromSlug: page.Slug,
			ToTree:   link.Tree,
			ToSlug:   link.Slug,
		}); err != nil {
			return Page{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Page{}, err
	}
	return pageFromRow(page), nil
}

// Get loads a page tip with outgoing links and backlinks.
func (s *Store) Get(ctx context.Context, tree, slug string) (Page, error) {
	slug = Slug(slug)
	row, err := s.db.Queries(ctx).GetWikiPage(ctx, &db.GetWikiPageParams{Tree: tree, Slug: slug})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Page{}, ErrNotFound
		}
		return Page{}, err
	}
	return s.decorate(ctx, pageFromRow(row))
}

// ListRecent returns pages newest-first.
func (s *Store) ListRecent(ctx context.Context, limit int) ([]Page, error) {
	pages, err := s.Pages(ctx, "")
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > len(pages) {
		limit = len(pages)
	}
	// Pages() is tree,slug order; sort by UpdatedAt desc for the index.
	out := append([]Page(nil), pages...)
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].UpdatedAt.After(out[i].UpdatedAt) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ListByTree lists one tree (empty tree = all).
func (s *Store) ListByTree(ctx context.Context, tree string) ([]Page, error) {
	return s.Pages(ctx, tree)
}

// Search runs websearch_to_tsquery against wiki_search. Empty tree means all trees.
func (s *Store) Search(ctx context.Context, query, tree string, limit int32) ([]SearchHit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 40
	}
	rows, err := s.db.Queries(ctx).SearchWikiPages(ctx, &db.SearchWikiPagesParams{
		Query:     query,
		Tree:      tree,
		PageLimit: limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]SearchHit, 0, len(rows))
	for _, r := range rows {
		out = append(out, SearchHit{
			Page: Page{
				Tree: r.Tree, Slug: r.Slug, Title: r.Title, Body: r.Body,
				Revision: r.Revision, CreatorID: r.CreatorID, ChannelID: r.ChannelID,
				UpdatedBy: r.UpdatedBy, UpdatedAt: r.UpdatedAt.Time,
			},
			Rank: r.Rank,
		})
	}
	return out, nil
}

// Pages lists pages, optionally filtered by tree (empty = all).
func (s *Store) Pages(ctx context.Context, tree string) ([]Page, error) {
	rows, err := s.db.Queries(ctx).ListWikiPages(ctx, tree)
	if err != nil {
		return nil, err
	}
	return pagesFromRows(rows), nil
}

// PagesForCreator lists pages tagged with a creator.
func (s *Store) PagesForCreator(ctx context.Context, creatorID pgtype.UUID) ([]Page, error) {
	rows, err := s.db.Queries(ctx).ListWikiPagesForCreator(ctx, creatorID)
	if err != nil {
		return nil, err
	}
	return pagesFromRows(rows), nil
}

// PagesForChannel lists pages tagged with a channel.
func (s *Store) PagesForChannel(ctx context.Context, channelID pgtype.UUID) ([]Page, error) {
	rows, err := s.db.Queries(ctx).ListWikiPagesForChannel(ctx, channelID)
	if err != nil {
		return nil, err
	}
	return pagesFromRows(rows), nil
}

// History returns revisions newest-first.
func (s *Store) History(ctx context.Context, tree, slug string) ([]Revision, error) {
	slug = Slug(slug)
	rows, err := s.db.Queries(ctx).ListWikiRevisions(ctx, &db.ListWikiRevisionsParams{Tree: tree, Slug: slug})
	if err != nil {
		return nil, err
	}
	out := make([]Revision, 0, len(rows))
	for _, r := range rows {
		out = append(out, revisionFromRow(r))
	}
	return out, nil
}

// Diff returns the unified diff between two revision bodies.
// fromRev/toRev of 0 mean the previous tip and tip (last two), respectively.
func (s *Store) Diff(ctx context.Context, tree, slug string, fromRev, toRev int32) (string, error) {
	slug = Slug(slug)
	q := s.db.Queries(ctx)
	if fromRev == 0 || toRev == 0 {
		rows, err := q.ListWikiRevisions(ctx, &db.ListWikiRevisionsParams{Tree: tree, Slug: slug})
		if err != nil {
			return "", err
		}
		if len(rows) == 0 {
			return "", fmt.Errorf("no revisions")
		}
		if toRev == 0 {
			toRev = rows[0].Revision
		}
		if fromRev == 0 {
			if len(rows) < 2 {
				fromRev = 0
			} else {
				fromRev = rows[1].Revision
			}
		}
	}

	var oldBody string
	if fromRev > 0 {
		from, err := q.GetWikiRevision(ctx, &db.GetWikiRevisionParams{Tree: tree, Slug: slug, Revision: fromRev})
		if err != nil {
			return "", err
		}
		oldBody = from.Body
	}
	to, err := q.GetWikiRevision(ctx, &db.GetWikiRevisionParams{Tree: tree, Slug: slug, Revision: toRev})
	if err != nil {
		return "", err
	}
	return UnifiedDiff(oldBody, to.Body), nil
}

func (s *Store) decorate(ctx context.Context, p Page) (Page, error) {
	q := s.db.Queries(ctx)
	links, err := q.ListWikiLinksFromPage(ctx, &db.ListWikiLinksFromPageParams{Tree: p.Tree, Slug: p.Slug})
	if err != nil {
		return p, err
	}
	p.Links = make([]Link, 0, len(links))
	for _, l := range links {
		p.Links = append(p.Links, Link{Tree: l.ToTree, Slug: l.ToSlug, Target: l.ToTree + "/" + l.ToSlug, Label: l.ToSlug})
	}
	backs, err := q.ListWikiBacklinks(ctx, &db.ListWikiBacklinksParams{Tree: p.Tree, Slug: p.Slug})
	if err != nil {
		return p, err
	}
	p.Backlinks = make([]Ref, 0, len(backs))
	for _, b := range backs {
		p.Backlinks = append(p.Backlinks, Ref{Tree: b.Tree, Slug: b.Slug, Title: b.Title})
	}
	return p, nil
}

func pageFromRow(r *db.WikiPage) Page {
	return Page{
		Tree: r.Tree, Slug: r.Slug, Title: r.Title, Body: r.Body,
		Revision: r.Revision, CreatorID: r.CreatorID, ChannelID: r.ChannelID,
		UpdatedBy: r.UpdatedBy, UpdatedAt: r.UpdatedAt.Time,
	}
}

func pagesFromRows(rows []*db.WikiPage) []Page {
	out := make([]Page, 0, len(rows))
	for _, r := range rows {
		out = append(out, pageFromRow(r))
	}
	return out
}

func revisionFromRow(r *db.WikiRevision) Revision {
	return Revision{
		ID: r.ID, Tree: r.Tree, Slug: r.Slug, Revision: r.Revision,
		Title: r.Title, Body: r.Body, Diff: r.Diff, Summary: r.Summary,
		ActorKind: r.ActorKind, ActorID: r.ActorID, SessionID: r.SessionID, ClientName: r.ClientName, CreatedAt: r.CreatedAt.Time,
	}
}
