package mcp

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/topics"
	"thirdcoast.systems/rewind/internal/wiki"
)

// WikiStore is the vault surface MCP tools call. Satisfied by internal/wiki at merge.
type WikiStore interface {
	Search(ctx context.Context, q, tree string, creatorID pgtype.UUID) ([]WikiSearchHit, error)
	Get(ctx context.Context, tree, slug string) (*WikiPage, error)
	PagesFor(ctx context.Context, creatorID, channelID pgtype.UUID) ([]WikiPage, error)
	Put(ctx context.Context, in WikiPutInput) (*WikiPage, error)
	History(ctx context.Context, tree, slug string) ([]WikiRevision, error)
	Diff(ctx context.Context, tree, slug string, fromRev, toRev int32) (string, error)
}

// Types mirror internal/wiki so the store leaf can satisfy WikiStore without mcp owning schema.

type WikiPage struct {
	Tree      string      `json:"tree"`
	Slug      string      `json:"slug"`
	Title     string      `json:"title"`
	Body      string      `json:"body,omitempty"`
	Revision  int32       `json:"revision"`
	CreatorID pgtype.UUID `json:"-"`
	ChannelID pgtype.UUID `json:"-"`
	UpdatedBy string      `json:"updated_by,omitempty"`
	UpdatedAt time.Time   `json:"updated_at,omitempty"`
}

type WikiRevision struct {
	Tree      string    `json:"tree"`
	Slug      string    `json:"slug"`
	Revision  int32     `json:"revision"`
	Title     string    `json:"title,omitempty"`
	Summary   string    `json:"summary"`
	Diff      string    `json:"diff,omitempty"`
	ActorKind string    `json:"actor_kind,omitempty"`
	ActorID   string    `json:"actor_id,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
}

type WikiSearchHit struct {
	WikiPage
	Rank float64 `json:"rank,omitempty"`
}

type WikiPutInput struct {
	Tree             string
	Slug             string
	Title            string
	Body             string
	ExpectedRevision int32
	Summary          string
	CreatorID        pgtype.UUID
	ChannelID        pgtype.UUID
	Actor            wikiActor
}

type wikiActor struct {
	Kind          string
	ID            string
	SessionID     string
	ClientName    string
	ClientVersion string
	TokenName     string
	UserID        pgtype.UUID
}

var (
	wikiStore     WikiStore
	wikiSlugClean = regexp.MustCompile(`[^a-z0-9]+`)
)

const wikiDiffTruncate = 4000

// SetWikiStore wires the real vault implementation after the store leaf merges.
func SetWikiStore(s WikiStore) { wikiStore = s }

func registerWikiTools(srv *mcpsdk.Server, dbc *db.DatabaseConnection) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "wiki_search", Description: "Full-text search vault pages. Optional tree and creator_id filters."}, wikiSearchMCP())
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "wiki_get", Description: "Get one vault page by tree+slug, or creator home via creator_id. Topic pages include inferred archive windows from context-window binds."}, wikiGetMCP(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "wiki_pages_for", Description: "List vault pages attached to a creator_id and/or channel_id."}, wikiPagesForMCP())
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "list_topic_windows", Description: "List playable context windows bound to a canonical topic (slug or search query). Cross-channel evidence; inferred from context windows, not wiki prose. Use this to hang clips and wiki citations across sources (e.g. a rant and a city-council meeting on the same subject)."}, listTopicWindowsMCP(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "wiki_put", Description: "Create or update a vault page with optimistic concurrency. Summary is required. Markdown body; inline media plays on the page: ![label](rewind://video/{uuid}), ![label](rewind://video/{uuid}#t=start,end), ![label](rewind://clip/{uuid}), ![still](rewind://video/{uuid}/thumbnail), ![frame](rewind://video/{uuid}/frame?t=seconds). A paragraph that is only a rewind://video or rewind://clip link also plays inline. Requires mcp:write."}, wikiPutMCP())
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "wiki_history", Description: "List vault page revisions newest first (summary, actor, truncated diff)."}, wikiHistoryMCP())
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "wiki_diff", Description: "Unified diff between two vault page revisions."}, wikiDiffMCP())
}

type wikiSearchArgs struct {
	Q         string `json:"q" jsonschema:"Full-text search query"`
	Tree      string `json:"tree,omitempty" jsonschema:"Optional tree: creator, channel, clipping, topic"`
	CreatorID string `json:"creator_id,omitempty" jsonschema:"Optional creator UUID filter"`
}

type wikiGetArgs struct {
	Tree      string `json:"tree,omitempty" jsonschema:"Page tree (required unless creator_id)"`
	Slug      string `json:"slug,omitempty" jsonschema:"Page slug (required unless creator_id)"`
	CreatorID string `json:"creator_id,omitempty" jsonschema:"Resolve creator home page creator/{slug}"`
}

type wikiPagesForArgs struct {
	CreatorID string `json:"creator_id,omitempty" jsonschema:"Creator UUID"`
	ChannelID string `json:"channel_id,omitempty" jsonschema:"Channel UUID"`
}

type wikiPutArgs struct {
	Tree             string `json:"tree" jsonschema:"Page tree: creator, channel, clipping, topic"`
	Slug             string `json:"slug" jsonschema:"Page slug (may include / for nested paths)"`
	Title            string `json:"title" jsonschema:"Page title"`
	Body             string `json:"body" jsonschema:"Markdown body. Inline media: ![label](rewind://video/{uuid}), ![label](rewind://video/{uuid}#t=start,end), ![label](rewind://clip/{uuid}), stills via /thumbnail or /frame?t=seconds."`
	ExpectedRevision int32  `json:"expected_revision" jsonschema:"Current revision; 0 creates a new page at revision 1"`
	Summary          string `json:"summary" jsonschema:"Required one-line changelog"`
	CreatorID        string `json:"creator_id,omitempty" jsonschema:"Optional attached creator UUID"`
	ChannelID        string `json:"channel_id,omitempty" jsonschema:"Optional attached channel UUID"`
}

type wikiHistoryArgs struct {
	Tree string `json:"tree" jsonschema:"Page tree"`
	Slug string `json:"slug" jsonschema:"Page slug"`
}

type wikiDiffArgs struct {
	Tree    string `json:"tree" jsonschema:"Page tree"`
	Slug    string `json:"slug" jsonschema:"Page slug"`
	FromRev int32  `json:"from_rev" jsonschema:"Older revision"`
	ToRev   int32  `json:"to_rev" jsonschema:"Newer revision"`
}

func wikiSearchMCP() func(context.Context, *mcpsdk.CallToolRequest, *wikiSearchArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *wikiSearchArgs) (*mcpsdk.CallToolResult, any, error) {
		store, err := requireWikiStore()
		if err != nil {
			return nil, nil, err
		}
		q := strings.TrimSpace(a.Q)
		if q == "" {
			return nil, nil, fmt.Errorf("q is required")
		}
		creatorID, err := optionalUUID(a.CreatorID)
		if err != nil {
			return nil, nil, err
		}
		hits, err := store.Search(ctx, q, strings.TrimSpace(a.Tree), creatorID)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{"pages": hits})
	}
}

func wikiGetMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *wikiGetArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *wikiGetArgs) (*mcpsdk.CallToolResult, any, error) {
		store, err := requireWikiStore()
		if err != nil {
			return nil, nil, err
		}
		tree := strings.TrimSpace(a.Tree)
		slug := strings.TrimSpace(a.Slug)
		if tree == "" || slug == "" {
			if strings.TrimSpace(a.CreatorID) == "" {
				return nil, nil, fmt.Errorf("tree and slug are required (or creator_id for home page)")
			}
			id, err := parseUUID(a.CreatorID)
			if err != nil {
				return nil, nil, err
			}
			if dbc == nil {
				return nil, nil, fmt.Errorf("tree and slug are required")
			}
			cr, err := dbc.Queries(ctx).GetCreator(ctx, id)
			if err != nil || cr == nil {
				return nil, nil, fmt.Errorf("creator not found")
			}
			tree = "creator"
			slug = wikiSlug(cr.Name)
			if slug == "" {
				return nil, nil, fmt.Errorf("creator name cannot be slugified")
			}
		}
		page, err := store.Get(ctx, tree, slug)
		if err != nil {
			return nil, nil, err
		}
		out := pageJSON(page)
		if tree == "topic" && dbc != nil {
			arch, aerr := topics.New(dbc).LoadArchive(ctx, slug, 25)
			if aerr == nil {
				out["archive"] = arch
			}
		}
		return jsonResult(out)
	}
}

type listTopicWindowsArgs struct {
	Slug  string `json:"slug,omitempty" jsonschema:"Canonical topic slug (e.g. flock-alpr)"`
	Query string `json:"query,omitempty" jsonschema:"Search topic title, slug, or alias when slug is unknown"`
	Limit int32  `json:"limit,omitempty" jsonschema:"Max windows, default 40"`
}

func listTopicWindowsMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *listTopicWindowsArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *listTopicWindowsArgs) (*mcpsdk.CallToolResult, any, error) {
		if dbc == nil {
			return nil, nil, fmt.Errorf("database unavailable")
		}
		limit := a.Limit
		if limit <= 0 {
			limit = 40
		}
		slug := strings.TrimSpace(a.Slug)
		q := strings.TrimSpace(a.Query)
		store := topics.New(dbc)
		if slug != "" {
			arch, err := store.LoadArchive(ctx, slug, limit)
			if err != nil {
				return nil, nil, err
			}
			return jsonResult(arch)
		}
		if q == "" {
			return nil, nil, fmt.Errorf("slug or query is required")
		}
		rows, err := dbc.Queries(ctx).SearchTopicWindows(ctx, &db.SearchTopicWindowsParams{
			TenantID: wiki.TenantFromContext(ctx), Query: q, AliasNorm: topics.Normalize(q), PageLimit: limit,
		})
		if err != nil {
			return nil, nil, err
		}
		windows := make([]topics.ArchiveWindow, 0, len(rows))
		seenTopic := map[string]string{}
		for _, r := range rows {
			if r == nil {
				continue
			}
			vid := uuidString(r.VideoID)
			seenTopic[r.TopicSlug] = r.TopicTitle
			windows = append(windows, topics.ArchiveWindow{
				ID: uuidString(r.ID), VideoID: vid, Title: r.Title, VideoTitle: r.VideoTitle,
				Uploader: r.Uploader, Start: r.StartTs, End: r.EndTs, MatchKind: r.MatchKind,
				URI: "rewind://video/" + vid, WebPath: fmt.Sprintf("/videos/%s?t=%.3f", vid, r.StartTs),
			})
		}
		return jsonResult(map[string]any{"query": q, "topics": seenTopic, "windows": windows})
	}
}

func wikiPagesForMCP() func(context.Context, *mcpsdk.CallToolRequest, *wikiPagesForArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *wikiPagesForArgs) (*mcpsdk.CallToolResult, any, error) {
		store, err := requireWikiStore()
		if err != nil {
			return nil, nil, err
		}
		creatorID, err := optionalUUID(a.CreatorID)
		if err != nil {
			return nil, nil, err
		}
		channelID, err := optionalUUID(a.ChannelID)
		if err != nil {
			return nil, nil, err
		}
		if !creatorID.Valid && !channelID.Valid {
			return nil, nil, fmt.Errorf("creator_id or channel_id is required")
		}
		pages, err := store.PagesFor(ctx, creatorID, channelID)
		if err != nil {
			return nil, nil, err
		}
		out := make([]map[string]any, 0, len(pages))
		for i := range pages {
			out = append(out, pageJSON(&pages[i]))
		}
		return jsonResult(map[string]any{"pages": out})
	}
}

func wikiPutMCP() func(context.Context, *mcpsdk.CallToolRequest, *wikiPutArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *wikiPutArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		summary := strings.TrimSpace(a.Summary)
		if summary == "" {
			return nil, nil, fmt.Errorf("summary is required")
		}
		tree := strings.TrimSpace(a.Tree)
		slug := strings.TrimSpace(a.Slug)
		if tree == "" || slug == "" {
			return nil, nil, fmt.Errorf("tree and slug are required")
		}
		store, err := requireWikiStore()
		if err != nil {
			return nil, nil, err
		}
		creatorID, err := optionalUUID(a.CreatorID)
		if err != nil {
			return nil, nil, err
		}
		channelID, err := optionalUUID(a.ChannelID)
		if err != nil {
			return nil, nil, err
		}
		page, err := store.Put(ctx, WikiPutInput{
			Tree:             tree,
			Slug:             slug,
			Title:            strings.TrimSpace(a.Title),
			Body:             a.Body,
			ExpectedRevision: a.ExpectedRevision,
			Summary:          summary,
			CreatorID:        creatorID,
			ChannelID:        channelID,
			Actor:            wikiActorFrom(ctx),
		})
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(pageJSON(page))
	}
}

func wikiHistoryMCP() func(context.Context, *mcpsdk.CallToolRequest, *wikiHistoryArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *wikiHistoryArgs) (*mcpsdk.CallToolResult, any, error) {
		store, err := requireWikiStore()
		if err != nil {
			return nil, nil, err
		}
		tree := strings.TrimSpace(a.Tree)
		slug := strings.TrimSpace(a.Slug)
		if tree == "" || slug == "" {
			return nil, nil, fmt.Errorf("tree and slug are required")
		}
		revs, err := store.History(ctx, tree, slug)
		if err != nil {
			return nil, nil, err
		}
		out := make([]map[string]any, 0, len(revs))
		for _, r := range revs {
			item := map[string]any{
				"tree":       r.Tree,
				"slug":       r.Slug,
				"revision":   r.Revision,
				"title":      r.Title,
				"summary":    r.Summary,
				"actor_kind": r.ActorKind,
				"actor_id":   r.ActorID,
				"created_at": r.CreatedAt,
				"diff":       truncateWikiDiff(r.Diff),
			}
			out = append(out, item)
		}
		return jsonResult(map[string]any{"revisions": out})
	}
}

func wikiDiffMCP() func(context.Context, *mcpsdk.CallToolRequest, *wikiDiffArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *wikiDiffArgs) (*mcpsdk.CallToolResult, any, error) {
		store, err := requireWikiStore()
		if err != nil {
			return nil, nil, err
		}
		tree := strings.TrimSpace(a.Tree)
		slug := strings.TrimSpace(a.Slug)
		if tree == "" || slug == "" {
			return nil, nil, fmt.Errorf("tree and slug are required")
		}
		diff, err := store.Diff(ctx, tree, slug, a.FromRev, a.ToRev)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{
			"tree":     tree,
			"slug":     slug,
			"from_rev": a.FromRev,
			"to_rev":   a.ToRev,
			"diff":     diff,
		})
	}
}

func requireWikiStore() (WikiStore, error) {
	if wikiStore == nil {
		return nil, fmt.Errorf("wiki store unavailable")
	}
	return wikiStore, nil
}

func wikiActorFrom(ctx context.Context) wikiActor {
	a := ActorFrom(ctx)
	return wikiActor{
		Kind:          string(a.Kind),
		ID:            a.ID,
		SessionID:     a.SessionID,
		ClientName:    a.ClientName,
		ClientVersion: a.ClientVersion,
		TokenName:     a.TokenName,
		UserID:        a.UserID,
	}
}

func wikiSlug(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	if s == "" {
		return ""
	}
	parts := strings.Split(s, "/")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = wikiSlugClean.ReplaceAllString(p, "-")
		p = strings.Trim(p, "-")
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "/")
}

func pageJSON(p *WikiPage) map[string]any {
	if p == nil {
		return nil
	}
	out := map[string]any{
		"tree":       p.Tree,
		"slug":       p.Slug,
		"title":      p.Title,
		"body":       p.Body,
		"revision":   p.Revision,
		"updated_by": p.UpdatedBy,
		"updated_at": p.UpdatedAt,
	}
	if p.CreatorID.Valid {
		out["creator_id"] = uuidString(p.CreatorID)
	}
	if p.ChannelID.Valid {
		out["channel_id"] = uuidString(p.ChannelID)
	}
	return out
}

func truncateWikiDiff(diff string) string {
	if len(diff) <= wikiDiffTruncate {
		return diff
	}
	return diff[:wikiDiffTruncate] + "\n…(truncated)"
}

func wikiSummaryFor(ctx context.Context, creatorID, channelID pgtype.UUID, homeHint string) any {
	if wikiStore == nil {
		return nil
	}
	pages, err := wikiStore.PagesFor(ctx, creatorID, channelID)
	if err != nil {
		return nil
	}
	var home, clipping *WikiPage
	for i := range pages {
		p := &pages[i]
		switch p.Tree {
		case "creator":
			if home == nil || (homeHint != "" && p.Slug == homeHint) {
				home = p
			}
		case "clipping":
			if clipping == nil {
				clipping = p
			}
		}
	}
	if home == nil && homeHint != "" {
		if p, err := wikiStore.Get(ctx, "creator", homeHint); err == nil {
			home = p
		}
	}
	if home == nil && clipping == nil {
		return nil
	}
	out := map[string]any{}
	if home != nil {
		out["home_slug"] = home.Tree + "/" + home.Slug
		out["revision"] = home.Revision
		out["updated_by"] = home.UpdatedBy
		out["updated_at"] = home.UpdatedAt
	}
	if clipping != nil {
		out["clipping_slug"] = clipping.Tree + "/" + clipping.Slug
		if home == nil {
			out["revision"] = clipping.Revision
			out["updated_by"] = clipping.UpdatedBy
			out["updated_at"] = clipping.UpdatedAt
		}
	}
	return out
}
