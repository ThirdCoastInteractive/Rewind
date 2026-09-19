package topics

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/wiki"
)

// Store resolves and persists topic binds against Postgres.
type Store struct {
	db *db.DatabaseConnection
}

// New returns a Store bound to dbc.
func New(dbc *db.DatabaseConnection) *Store {
	return &Store{db: dbc}
}

type dbCatalog struct {
	ctx context.Context
	q   *db.Queries
	mem *memCat
}

func newDBCatalog(ctx context.Context, q *db.Queries) *dbCatalog {
	return &dbCatalog{ctx: ctx, q: q, mem: newMem("", "")}
}

func (c *dbCatalog) Lookup(norm string) (string, string, bool) {
	if slug, title, ok := c.mem.Lookup(norm); ok {
		return slug, title, true
	}
	row, err := c.q.GetTopicByAlias(c.ctx, norm)
	if err != nil {
		return "", "", false
	}
	c.mem.EnsureTopic(row.Slug, row.Title, row.Origin)
	c.mem.EnsureAlias(norm, row.Slug, norm, "seed")
	return row.Slug, row.Title, true
}

func (c *dbCatalog) EnsureTopic(slug, title, origin string) (string, bool) {
	if t, ok := c.mem.title[slug]; ok {
		return t, false
	}
	row, err := c.q.UpsertTopic(c.ctx, &db.UpsertTopicParams{Slug: slug, Title: title, Origin: origin})
	if err != nil {
		slog.Warn("upsert topic", "slug", slug, "error", err)
		return title, false
	}
	_, created := c.mem.EnsureTopic(row.Slug, row.Title, row.Origin)
	return row.Title, created
}

func (c *dbCatalog) EnsureAlias(norm, slug, raw, source string) {
	c.mem.EnsureAlias(norm, slug, raw, source)
	if err := c.q.InsertTopicAlias(c.ctx, &db.InsertTopicAliasParams{
		AliasNorm: norm, TopicSlug: slug, Raw: raw, Source: source,
	}); err != nil {
		slog.Warn("insert topic alias", "alias", norm, "error", err)
	}
}

// SeedWiki loads topic-tree wiki pages into the catalog.
func (s *Store) SeedWiki(ctx context.Context) error {
	if s == nil || s.db == nil {
		return nil
	}
	q := s.db.Queries(ctx)
	pages, err := q.ListWikiPages(ctx, wiki.TreeTopic)
	if err != nil {
		return err
	}
	cat := newDBCatalog(ctx, q)
	for _, p := range pages {
		if p == nil {
			continue
		}
		cat.EnsureTopic(p.Slug, p.Title, "wiki")
		for _, a := range aliasesFor(p.Slug, p.Title) {
			cat.EnsureAlias(a.Norm, p.Slug, a.Raw, a.Source)
		}
	}
	return nil
}

// BindWindow resolves one chapter window and writes binds. Call SeedWiki first
// when wiki topic pages may have changed.
func (s *Store) BindWindow(ctx context.Context, id pgtype.UUID, title string, topicLabels, entities []string) error {
	if s == nil || s.db == nil || !id.Valid {
		return nil
	}
	q := s.db.Queries(ctx)
	cat := newDBCatalog(ctx, q)
	binds := Resolve(Window{Title: title, Topics: topicLabels, Entities: entities}, cat)
	if err := q.DeleteWindowTopics(ctx, id); err != nil {
		return err
	}
	for _, b := range binds {
		if err := q.BindWindowTopic(ctx, &db.BindWindowTopicParams{
			WindowID: id, TopicSlug: b.Slug, Raw: b.Raw, MatchKind: b.MatchKind,
		}); err != nil {
			return err
		}
	}
	return q.MarkWindowTopicsResolved(ctx, id)
}

// Backfill resolves unbound live chapter windows in batches.
func (s *Store) Backfill(ctx context.Context, limit int32) (int, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	if limit <= 0 {
		limit = 50
	}
	q := s.db.Queries(ctx)
	rows, err := q.ListWindowsForTopicBind(ctx, limit)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, s.EnsureStubs(ctx)
	}
	if err := s.SeedWiki(ctx); err != nil {
		return 0, err
	}
	n := 0
	for _, row := range rows {
		if row == nil {
			continue
		}
		if err := s.BindWindow(ctx, row.ID, row.Title, row.Topics, row.Entities); err != nil {
			return n, err
		}
		n++
	}
	if err := s.EnsureStubs(ctx); err != nil {
		return n, err
	}
	return n, nil
}

// EnsureStubs creates empty wiki topic pages for identities seen on 2+ channels.
func (s *Store) EnsureStubs(ctx context.Context) error {
	if s == nil || s.db == nil {
		return nil
	}
	q := s.db.Queries(ctx)
	need, err := q.ListTopicsNeedingStub(ctx)
	if err != nil {
		return err
	}
	store := wiki.New(s.db)
	for _, t := range need {
		if t == nil {
			continue
		}
		_, err := store.Get(ctx, wiki.TreeTopic, t.Slug)
		if err == nil {
			continue
		}
		if !errors.Is(err, wiki.ErrNotFound) {
			return err
		}
		if _, err := store.Put(ctx, wiki.PutInput{
			Tree:             wiki.TreeTopic,
			Slug:             t.Slug,
			Title:            t.Title,
			Body:             "",
			ExpectedRevision: 0,
			Summary:          "auto-stub from context windows",
			Actor:            wiki.Actor{Kind: "system"},
		}); err != nil {
			if errors.Is(err, wiki.ErrConflict) {
				continue
			}
			return fmt.Errorf("stub %s: %w", t.Slug, err)
		}
		if _, err := q.UpsertTopic(ctx, &db.UpsertTopicParams{Slug: t.Slug, Title: t.Title, Origin: "stub"}); err != nil {
			return err
		}
	}
	return nil
}

// CatalogPrompt is a compact list of known topic titles for the context-window model.
func CatalogPrompt(ctx context.Context, q *db.Queries) string {
	if q == nil {
		return ""
	}
	rows, err := q.ListTopics(ctx, 80)
	if err != nil || len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Known durable topics (prefer these names when they apply; do not invent close paraphrases):\n")
	for _, t := range rows {
		if t == nil {
			continue
		}
		b.WriteString("- ")
		b.WriteString(t.Title)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// ArchiveSummary is the live evidence payload for a topic wiki page / MCP.
type ArchiveSummary struct {
	Slug         string          `json:"slug"`
	Title        string          `json:"title"`
	WindowCount  int64           `json:"window_count"`
	ChannelCount int64           `json:"channel_count"`
	Windows      []ArchiveWindow `json:"windows"`
	Related      []RelatedTopic  `json:"related"`
}

// ArchiveWindow is one bound chapter window.
type ArchiveWindow struct {
	ID         string  `json:"id"`
	VideoID    string  `json:"video_id"`
	Title      string  `json:"title"`
	VideoTitle string  `json:"video_title"`
	Uploader   string  `json:"uploader"`
	Start      float64 `json:"start"`
	End        float64 `json:"end"`
	MatchKind  string  `json:"match_kind"`
	URI        string  `json:"uri"`
	WebPath    string  `json:"web_path"`
}

// RelatedTopic is a co-occurring catalog identity.
type RelatedTopic struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
	Count int64  `json:"count"`
}

// LoadArchive returns inferred evidence for a topic slug.
func (s *Store) LoadArchive(ctx context.Context, slug string, limit int32) (ArchiveSummary, error) {
	out := ArchiveSummary{Slug: slug}
	if s == nil || s.db == nil || slug == "" {
		return out, nil
	}
	if limit <= 0 {
		limit = 40
	}
	q := s.db.Queries(ctx)
	if t, err := q.GetTopic(ctx, slug); err == nil && t != nil {
		out.Title = t.Title
	}
	if n, err := q.CountTopicWindows(ctx, slug); err == nil {
		out.WindowCount = n
	}
	if n, err := q.CountTopicChannels(ctx, slug); err == nil {
		out.ChannelCount = n
	}
	rows, err := q.ListTopicWindows(ctx, &db.ListTopicWindowsParams{Slug: slug, PageLimit: limit})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	for _, r := range rows {
		if r == nil {
			continue
		}
		vid := uuidString(r.VideoID)
		out.Windows = append(out.Windows, ArchiveWindow{
			ID: uuidString(r.ID), VideoID: vid, Title: r.Title, VideoTitle: r.VideoTitle,
			Uploader: r.Uploader, Start: r.StartTs, End: r.EndTs, MatchKind: r.MatchKind,
			URI: "rewind://video/" + vid, WebPath: fmt.Sprintf("/videos/%s?t=%.3f", vid, r.StartTs),
		})
	}
	rel, err := q.ListRelatedTopics(ctx, slug)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	for _, r := range rel {
		if r == nil {
			continue
		}
		out.Related = append(out.Related, RelatedTopic{Slug: r.Slug, Title: r.Title, Count: r.N})
	}
	return out, nil
}

func uuidString(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	return uuid.UUID(id.Bytes).String()
}
