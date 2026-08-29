// Package creatorlink groups channels onto creators from harvested outlinks:
// labeled "main/alt channel" references, and mutual outlinks between archived
// channels. High-confidence clusters are applied; weaker one-way links are
// stored as nominations.
package creatorlink

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/channelid"
	"thirdcoast.systems/rewind/internal/db"
)

// Apply materializes creator groupings from the current channel graph.
func Apply(ctx context.Context, q *db.Queries) {
	if q == nil {
		return
	}
	if err := materializeLabeledTargets(ctx, q); err != nil {
		slog.Warn("creatorlink: materialize labeled targets", "error", err)
	}
	if err := q.ResolveChannelEdges(ctx); err != nil {
		slog.Warn("creatorlink: resolve edges", "error", err)
	}
	if err := applyClusters(ctx, q); err != nil {
		slog.Warn("creatorlink: apply clusters", "error", err)
	}
	if err := nominateOneWay(ctx, q); err != nil {
		slog.Warn("creatorlink: nominate", "error", err)
	}
}

func materializeLabeledTargets(ctx context.Context, q *db.Queries) error {
	rows, err := q.ListLabeledUnresolvedOutlinks(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row == nil || RoleFromEvidence(row.Evidence) == "" {
			continue
		}
		id := channelid.FromURL(row.ToURL)
		uploader := strings.TrimSpace(id.Uploader)
		if uploader == "" {
			uploader = handleName("", row.ToURL)
		}
		if _, err := q.UpsertChannel(ctx, &db.UpsertChannelParams{
			Platform:     id.Platform,
			IdentityKey:  id.Key,
			ChannelID:    id.ChannelID,
			Uploader:     uploader,
			CanonicalURL: id.CanonicalURL,
		}); err != nil {
			slog.Warn("creatorlink: upsert target channel", "url", row.ToURL, "error", err)
		}
	}
	return nil
}

type member struct {
	id        pgtype.UUID
	uploader  string
	creatorID pgtype.UUID
}

func applyClusters(ctx context.Context, q *db.Queries) error {
	uf := newUnion()
	nodes := map[string]member{}

	remember := func(id pgtype.UUID, uploader string, creator pgtype.UUID) {
		key := uuidKey(id)
		if key == "" {
			return
		}
		if _, ok := nodes[key]; !ok {
			nodes[key] = member{id: id, uploader: uploader, creatorID: creator}
		}
		uf.add(key)
	}

	pairs, err := q.ListMutualOutlinkPairs(ctx)
	if err != nil {
		return err
	}
	for _, p := range pairs {
		if p == nil {
			continue
		}
		remember(p.AID, p.AUploader, p.ACreatorID)
		remember(p.BID, p.BUploader, p.BCreatorID)
		if a, b := uuidKey(p.AID), uuidKey(p.BID); a != "" && b != "" {
			uf.union(a, b)
		}
	}

	outlinks, err := q.ListResolvedOutlinks(ctx)
	if err != nil {
		return err
	}
	for _, e := range outlinks {
		if e == nil || RoleFromEvidence(e.Evidence) == "" {
			continue
		}
		remember(e.FromChannelID, e.FromUploader, e.FromCreatorID)
		remember(e.ToChannelID, e.ToUploader, e.ToCreatorID)
		if a, b := uuidKey(e.FromChannelID), uuidKey(e.ToChannelID); a != "" && b != "" {
			uf.union(a, b)
		}
	}

	unresolved, err := q.ListLabeledUnresolvedOutlinks(ctx)
	if err != nil {
		return err
	}
	for _, row := range unresolved {
		if row == nil || RoleFromEvidence(row.Evidence) == "" {
			continue
		}
		id := channelid.FromURL(row.ToURL)
		ch, err := q.GetChannelByIdentity(ctx, &db.GetChannelByIdentityParams{
			Platform:    id.Platform,
			IdentityKey: id.Key,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) && id.CanonicalURL != "" {
				ch, err = q.GetChannelByCanonicalURL(ctx, id.CanonicalURL)
			}
			if err != nil {
				continue
			}
		}
		if ch == nil {
			continue
		}
		remember(row.FromChannelID, row.FromUploader, row.FromCreatorID)
		remember(ch.ID, ch.Uploader, ch.CreatorID)
		if a, b := uuidKey(row.FromChannelID), uuidKey(ch.ID); a != "" && b != "" {
			uf.union(a, b)
		}
	}

	groups := uf.groups()
	for _, ids := range groups {
		if len(ids) < 2 {
			continue
		}
		members := make([]member, 0, len(ids))
		for _, id := range ids {
			members = append(members, nodes[id])
		}
		if err := applyCluster(ctx, q, members); err != nil {
			slog.Warn("creatorlink: cluster", "error", err)
		}
	}
	return nil
}

func applyCluster(ctx context.Context, q *db.Queries, members []member) error {
	var creators []pgtype.UUID
	seen := map[string]struct{}{}
	unassigned := make([]pgtype.UUID, 0, len(members))
	name := clusterName(members)
	for _, m := range members {
		if k := uuidKey(m.creatorID); k != "" {
			if _, ok := seen[k]; !ok {
				seen[k] = struct{}{}
				creators = append(creators, m.creatorID)
			}
			continue
		}
		if m.id.Valid {
			unassigned = append(unassigned, m.id)
		}
	}
	if len(creators) >= 2 {
		return nominate(ctx, q, "new", pgtype.UUID{}, members, name, "channels in this cluster already belong to different creators")
	}
	if len(creators) == 1 {
		if len(unassigned) == 0 {
			return nil
		}
		return q.LinkChannelsToCreator(ctx, &db.LinkChannelsToCreatorParams{
			CreatorID: creators[0],
			Ids:       unassigned,
		})
	}
	if strings.TrimSpace(name) == "" {
		name = "Untitled creator"
	}
	existing, err := q.GetCreatorByNameCI(ctx, name)
	var creator *db.Creator
	if err == nil {
		creator = existing
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if creator == nil {
		notes := "auto-grouped from channel outlinks"
		created, err := q.CreateCreator(ctx, &db.CreateCreatorParams{Name: name, Notes: &notes})
		if err != nil {
			return err
		}
		creator = created
	}
	ids := memberIDs(members)
	if len(ids) == 0 {
		return nil
	}
	return q.LinkChannelsToCreator(ctx, &db.LinkChannelsToCreatorParams{
		CreatorID: creator.ID,
		Ids:       ids,
	})
}

func nominateOneWay(ctx context.Context, q *db.Queries) error {
	rows, err := q.ListResolvedOutlinks(ctx)
	if err != nil {
		return err
	}
	for _, e := range rows {
		if e == nil {
			continue
		}
		if e.FromCreatorID.Valid && e.ToCreatorID.Valid {
			continue
		}
		if RoleFromEvidence(e.Evidence) != "" {
			continue
		}
		if e.FromCreatorID.Valid && !e.ToCreatorID.Valid && e.ToChannelID.Valid {
			_ = nominate(ctx, q, "add", e.FromCreatorID, []member{{id: e.ToChannelID, uploader: e.ToUploader}}, e.FromUploader, "outlink from "+e.FromUploader)
			continue
		}
		if e.ToCreatorID.Valid && !e.FromCreatorID.Valid && e.FromChannelID.Valid {
			_ = nominate(ctx, q, "add", e.ToCreatorID, []member{{id: e.FromChannelID, uploader: e.FromUploader}}, e.ToUploader, "outlink to "+e.ToUploader)
		}
	}
	return nil
}

func nominate(ctx context.Context, q *db.Queries, kind string, creatorID pgtype.UUID, members []member, name, reason string) error {
	ids := make([]string, 0, len(members))
	for _, m := range members {
		if k := uuidKey(m.id); k != "" {
			ids = append(ids, k)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	sort.Strings(ids)
	key := kind + ":" + strings.Join(ids, ",")
	if existing, err := q.GetCreatorSuggestionByKey(ctx, key); err == nil && existing != nil {
		return nil
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if strings.TrimSpace(name) == "" {
		name = "Untitled creator"
	}
	sug, err := q.InsertCreatorSuggestion(ctx, &db.InsertCreatorSuggestionParams{
		Kind:         kind,
		CreatorID:    creatorID,
		ProposedName: name,
		Reason:       reason,
		Evidence:     "",
		ChannelKey:   key,
	})
	if err != nil {
		if db.IsUniqueViolationErr(err) {
			return nil
		}
		return err
	}
	for _, m := range members {
		if !m.id.Valid {
			continue
		}
		if err := q.AddCreatorSuggestionMember(ctx, &db.AddCreatorSuggestionMemberParams{
			SuggestionID: sug.ID,
			ChannelID:    m.id,
		}); err != nil {
			return err
		}
	}
	return nil
}

func uuidKey(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	return id.String()
}

func memberIDs(members []member) []pgtype.UUID {
	ids := make([]pgtype.UUID, 0, len(members))
	for _, m := range members {
		if m.id.Valid {
			ids = append(ids, m.id)
		}
	}
	return ids
}

func clusterName(members []member) string {
	best := ""
	for _, m := range members {
		u := strings.TrimSpace(m.uploader)
		if u == "" {
			continue
		}
		if betterClusterName(u, best) {
			best = u
		}
	}
	return best
}

func betterClusterName(candidate, current string) bool {
	if current == "" {
		return true
	}
	cSpace, nSpace := strings.Contains(candidate, " "), strings.Contains(current, " ")
	if cSpace != nSpace {
		return cSpace
	}
	if len(candidate) != len(current) {
		return len(candidate) > len(current)
	}
	return candidate < current
}

type unionFind struct {
	parent map[string]string
}

func newUnion() *unionFind {
	return &unionFind{parent: map[string]string{}}
}

func (u *unionFind) add(x string) {
	if _, ok := u.parent[x]; !ok {
		u.parent[x] = x
	}
}

func (u *unionFind) find(x string) string {
	p := u.parent[x]
	if p != x {
		u.parent[x] = u.find(p)
	}
	return u.parent[x]
}

func (u *unionFind) union(a, b string) {
	u.add(a)
	u.add(b)
	pa, pb := u.find(a), u.find(b)
	if pa != pb {
		u.parent[pa] = pb
	}
}

func (u *unionFind) groups() [][]string {
	g := map[string][]string{}
	for x := range u.parent {
		root := u.find(x)
		g[root] = append(g[root], x)
	}
	out := make([][]string, 0, len(g))
	for _, ids := range g {
		out = append(out, ids)
	}
	return out
}
