package mcp

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/wiki"
)

type wikiStoreAdapter struct {
	inner *wiki.Store
}

func newWikiStoreAdapter(s *wiki.Store) WikiStore {
	return wikiStoreAdapter{inner: s}
}

func (a wikiStoreAdapter) Search(ctx context.Context, q, tree string, creatorID pgtype.UUID) ([]WikiSearchHit, error) {
	hits, err := a.inner.Search(ctx, q, tree, 40)
	if err != nil {
		return nil, err
	}
	out := make([]WikiSearchHit, 0, len(hits))
	for _, h := range hits {
		if creatorID.Valid && h.CreatorID != creatorID {
			continue
		}
		out = append(out, WikiSearchHit{WikiPage: mcpPage(h.Page), Rank: h.Rank})
	}
	return out, nil
}

func (a wikiStoreAdapter) Get(ctx context.Context, tree, slug string) (*WikiPage, error) {
	p, err := a.inner.Get(ctx, tree, slug)
	if err != nil {
		return nil, err
	}
	out := mcpPage(p)
	return &out, nil
}

func (a wikiStoreAdapter) PagesFor(ctx context.Context, creatorID, channelID pgtype.UUID) ([]WikiPage, error) {
	var pages []wiki.Page
	if creatorID.Valid {
		list, err := a.inner.PagesForCreator(ctx, creatorID)
		if err != nil {
			return nil, err
		}
		pages = append(pages, list...)
	}
	if channelID.Valid {
		list, err := a.inner.PagesForChannel(ctx, channelID)
		if err != nil {
			return nil, err
		}
		pages = append(pages, list...)
	}
	out := make([]WikiPage, 0, len(pages))
	seen := map[string]bool{}
	for _, p := range pages {
		k := p.Tree + "/" + p.Slug
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, mcpPage(p))
	}
	return out, nil
}

func (a wikiStoreAdapter) Put(ctx context.Context, in WikiPutInput) (*WikiPage, error) {
	p, err := a.inner.Put(ctx, wiki.PutInput{
		Tree:             in.Tree,
		Slug:             in.Slug,
		Title:            in.Title,
		Body:             in.Body,
		ExpectedRevision: in.ExpectedRevision,
		Summary:          in.Summary,
		CreatorID:        in.CreatorID,
		ChannelID:        in.ChannelID,
		Actor: wiki.Actor{
			Kind:          in.Actor.Kind,
			ID:            in.Actor.ID,
			SessionID:     in.Actor.SessionID,
			ClientName:    in.Actor.ClientName,
			ClientVersion: in.Actor.ClientVersion,
			TokenName:     in.Actor.TokenName,
			UserID:        in.Actor.UserID,
		},
	})
	if err != nil {
		return nil, err
	}
	out := mcpPage(p)
	return &out, nil
}

func (a wikiStoreAdapter) History(ctx context.Context, tree, slug string) ([]WikiRevision, error) {
	revs, err := a.inner.History(ctx, tree, slug)
	if err != nil {
		return nil, err
	}
	out := make([]WikiRevision, 0, len(revs))
	for _, r := range revs {
		out = append(out, WikiRevision{
			Tree: r.Tree, Slug: r.Slug, Revision: r.Revision, Title: r.Title,
			Summary: r.Summary, Diff: r.Diff, ActorKind: r.ActorKind, ActorID: r.ActorID,
			CreatedAt: r.CreatedAt,
		})
	}
	return out, nil
}

func (a wikiStoreAdapter) Diff(ctx context.Context, tree, slug string, fromRev, toRev int32) (string, error) {
	return a.inner.Diff(ctx, tree, slug, fromRev, toRev)
}

func mcpPage(p wiki.Page) WikiPage {
	return WikiPage{
		Tree: p.Tree, Slug: p.Slug, Title: p.Title, Body: p.Body,
		Revision: p.Revision, CreatorID: p.CreatorID, ChannelID: p.ChannelID,
		UpdatedBy: p.UpdatedBy, UpdatedAt: p.UpdatedAt,
	}
}
