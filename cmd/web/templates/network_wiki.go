package templates

import (
	"strings"

	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/wiki"
)

func attachWikiToGraph(graph *networkGraph, pages []*db.WikiPage, links []*db.WikiLink, allowCreators, allowChannels map[string]bool) {
	if graph == nil {
		return
	}
	byKey := map[string]*db.WikiPage{}
	for _, p := range pages {
		if p == nil || (!p.CreatorID.Valid && !p.ChannelID.Valid) {
			continue
		}
		byKey[p.Tree+"/"+p.Slug] = p
	}
	allowed := map[string]bool{}
	filtered := allowCreators != nil || allowChannels != nil
	for key, p := range byKey {
		if !filtered || wikiPageAllowed(p, allowCreators, allowChannels) {
			allowed[key] = true
		}
	}
	if filtered {
		for _, l := range links {
			if l == nil {
				continue
			}
			from, to := l.FromTree+"/"+l.FromSlug, l.ToTree+"/"+l.ToSlug
			if allowed[from] || allowed[to] {
				if _, ok := byKey[from]; ok {
					allowed[from] = true
				}
				if _, ok := byKey[to]; ok {
					allowed[to] = true
				}
			}
		}
	}

	byID := map[string]int{}
	creatorsPresent := map[string]bool{}
	for i, n := range graph.Nodes {
		byID[n.ID] = i
		if n.CreatorID != "" {
			creatorsPresent[n.CreatorID] = true
		}
	}

	searchByCreator := map[string]string{}
	searchByChannel := map[string]string{}
	for key, p := range byKey {
		if !allowed[key] {
			continue
		}
		if p.CreatorID.Valid {
			id := p.CreatorID.String()
			searchByCreator[id] = strings.TrimSpace(searchByCreator[id] + " " + p.Title + " " + p.Slug)
			if !creatorsPresent[id] {
				nodeID := "creator:" + id
				if _, ok := byID[nodeID]; !ok {
					n := networkNode{
						ID:          nodeID,
						Label:       wikiNodeLabel(p),
						Platform:    "vault",
						Href:        wiki.PageURL(p.Tree, p.Slug),
						CreatorID:   id,
						CreatorName: wikiNodeLabel(p),
						Search:      p.Title + " " + p.Slug,
					}
					byID[nodeID] = len(graph.Nodes)
					graph.Nodes = append(graph.Nodes, n)
					creatorsPresent[id] = true
				}
			}
		}
		if p.ChannelID.Valid {
			searchByChannel[p.ChannelID.String()] = strings.TrimSpace(searchByChannel[p.ChannelID.String()] + " " + p.Title + " " + p.Slug)
		}
	}
	for i, n := range graph.Nodes {
		extra := ""
		if n.CreatorID != "" {
			extra = searchByCreator[n.CreatorID]
		}
		if s := searchByChannel[n.ID]; s != "" {
			extra = strings.TrimSpace(extra + " " + s)
		}
		if extra != "" {
			graph.Nodes[i].Search = strings.TrimSpace(n.Search + " " + extra)
		}
	}

	seen := map[string]bool{}
	for _, l := range links {
		if l == nil {
			continue
		}
		fromPage, toPage := byKey[l.FromTree+"/"+l.FromSlug], byKey[l.ToTree+"/"+l.ToSlug]
		if fromPage == nil || toPage == nil {
			continue
		}
		if !allowed[l.FromTree+"/"+l.FromSlug] && !allowed[l.ToTree+"/"+l.ToSlug] {
			continue
		}
		fromID := wikiEndpoint(fromPage)
		toID := wikiEndpoint(toPage)
		if fromID == "" || toID == "" || fromID == toID {
			continue
		}
		if !wikiEndpointPresent(fromID, byID, creatorsPresent) || !wikiEndpointPresent(toID, byID, creatorsPresent) {
			continue
		}
		edgeID := "wiki:" + l.FromTree + "/" + l.FromSlug + ">" + l.ToTree + "/" + l.ToSlug
		if seen[edgeID] {
			continue
		}
		seen[edgeID] = true
		graph.Links = append(graph.Links, networkLink{
			ID:       edgeID,
			Source:   fromID,
			Target:   toID,
			Kind:     "wiki",
			Weight:   1,
			Evidence: fromPage.Title + " → " + toPage.Title,
			Href:     wiki.PageURL(l.FromTree, l.FromSlug),
		})
	}
}

func wikiPageAllowed(p *db.WikiPage, allowCreators, allowChannels map[string]bool) bool {
	if p.CreatorID.Valid && allowCreators[p.CreatorID.String()] {
		return true
	}
	if p.ChannelID.Valid && allowChannels[p.ChannelID.String()] {
		return true
	}
	return false
}

func wikiEndpoint(p *db.WikiPage) string {
	if p.ChannelID.Valid {
		return p.ChannelID.String()
	}
	if p.CreatorID.Valid {
		return "creator:" + p.CreatorID.String()
	}
	return ""
}

func wikiEndpointPresent(id string, byID map[string]int, creatorsPresent map[string]bool) bool {
	if _, ok := byID[id]; ok {
		return true
	}
	if strings.HasPrefix(id, "creator:") && creatorsPresent[strings.TrimPrefix(id, "creator:")] {
		return true
	}
	return false
}

func wikiNodeLabel(p *db.WikiPage) string {
	if strings.TrimSpace(p.Title) != "" {
		return p.Title
	}
	return p.Slug
}
