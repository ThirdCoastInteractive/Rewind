package templates

import (
	"encoding/json"
	"sort"
	"strings"
)

// InvestigateGraph is the desk's node/edge map of actors and campaigns.
type InvestigateGraph struct {
	Nodes []InvestigateGraphNode `json:"nodes"`
	Links []InvestigateGraphLink `json:"links"`
}

type InvestigateGraphNode struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"` // commenter | campaign
	Label string `json:"label"`
	Href  string `json:"href"`
	Meta  string `json:"meta,omitempty"`
}

type InvestigateGraphLink struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Kind   string `json:"kind"`
}

type InvestigateGraphEdge struct {
	From      string
	To        string
	Kind      string
	FromLabel string
	ToLabel   string
}

// BuildInvestigateGraph turns watchlist, flags, campaigns, and extra edges into a graph.
func BuildInvestigateGraph(watch []InvestigateWatchItem, flags []InvestigateFlagItem, camps []InvestigateCampaignItem, extra []InvestigateGraphEdge) InvestigateGraph {
	nodes := map[string]InvestigateGraphNode{}
	addCommenter := func(id, label, meta string) {
		if id == "" {
			return
		}
		key := "commenter:" + id
		if existing, ok := nodes[key]; ok {
			if label != "" && (existing.Label == "" || existing.Label == id) {
				existing.Label = label
			}
			if existing.Meta == "watch" || meta == "" {
				nodes[key] = existing
				return
			}
			if existing.Meta == "" {
				existing.Meta = meta
				nodes[key] = existing
			}
			return
		}
		if label == "" {
			label = id
		}
		nodes[key] = InvestigateGraphNode{
			ID: key, Kind: "commenter", Label: label,
			Href: "/investigate/commenters/" + id, Meta: meta,
		}
	}
	addCampaign := func(id, label, kind string) {
		if id == "" {
			return
		}
		key := "campaign:" + id
		if _, ok := nodes[key]; ok && label == "" {
			return
		}
		if label == "" {
			label = kind
		}
		nodes[key] = InvestigateGraphNode{
			ID: key, Kind: "campaign", Label: label,
			Href: "/investigate/campaigns/" + id, Meta: kind,
		}
	}
	for _, w := range watch {
		label := w.DisplayName
		if label == "" {
			label = w.AuthorID
		}
		addCommenter(w.CommenterID, label, "watch")
	}
	for _, f := range flags {
		addCommenter(f.CommenterID, f.CommenterName, f.Kind)
		if f.CampaignID != "" {
			addCampaign(f.CampaignID, "", f.Kind)
		}
	}
	for _, c := range camps {
		addCampaign(c.ID, c.NormalizedText, c.Kind)
	}
	for _, e := range extra {
		if id, ok := strings.CutPrefix(e.From, "commenter:"); ok {
			addCommenter(id, e.FromLabel, e.Kind)
		}
		if id, ok := strings.CutPrefix(e.To, "commenter:"); ok {
			addCommenter(id, e.ToLabel, e.Kind)
		}
		if id, ok := strings.CutPrefix(e.From, "campaign:"); ok {
			addCampaign(id, e.FromLabel, e.Kind)
		}
		if id, ok := strings.CutPrefix(e.To, "campaign:"); ok {
			addCampaign(id, e.ToLabel, e.Kind)
		}
	}
	seen := map[string]bool{}
	var links []InvestigateGraphLink
	addLink := func(from, to, kind string) {
		if from == "" || to == "" || from == to || kind == "" {
			return
		}
		if _, ok := nodes[from]; !ok {
			return
		}
		if _, ok := nodes[to]; !ok {
			return
		}
		key := from + ">" + to + ">" + kind
		if seen[key] {
			return
		}
		seen[key] = true
		links = append(links, InvestigateGraphLink{Source: from, Target: to, Kind: kind})
	}
	for _, f := range flags {
		if f.CommenterID != "" && f.CampaignID != "" {
			addLink("commenter:"+f.CommenterID, "campaign:"+f.CampaignID, f.Kind)
		}
	}
	for _, e := range extra {
		addLink(e.From, e.To, e.Kind)
	}
	out := InvestigateGraph{
		Nodes: make([]InvestigateGraphNode, 0, len(nodes)),
		Links: links,
	}
	if out.Links == nil {
		out.Links = []InvestigateGraphLink{}
	}
	for _, n := range nodes {
		out.Nodes = append(out.Nodes, n)
	}
	return out
}

// EdgesFromFlagEvidence turns sock_suggest flags into style edges.
func EdgesFromFlagEvidence(flags []InvestigateFlagItem) []InvestigateGraphEdge {
	var extra []InvestigateGraphEdge
	for _, f := range flags {
		if f.Kind != "sock_suggest" || f.CommenterID == "" || f.PeerID == "" || f.CommenterID == f.PeerID {
			continue
		}
		extra = append(extra, InvestigateGraphEdge{
			From: "commenter:" + f.CommenterID, To: "commenter:" + f.PeerID, Kind: "style",
			FromLabel: f.CommenterName,
		})
	}
	return extra
}

// PruneInvestigateGraph keeps watch nodes, then highest-degree neighbors, up to maxNodes.
func PruneInvestigateGraph(g InvestigateGraph, maxNodes int) InvestigateGraph {
	if maxNodes < 1 || len(g.Nodes) <= maxNodes {
		return g
	}
	degree := map[string]int{}
	adj := map[string][]string{}
	for _, l := range g.Links {
		degree[l.Source]++
		degree[l.Target]++
		adj[l.Source] = append(adj[l.Source], l.Target)
		adj[l.Target] = append(adj[l.Target], l.Source)
	}
	keep := map[string]bool{}
	add := func(id string) bool {
		if id == "" || keep[id] {
			return keep[id]
		}
		if len(keep) >= maxNodes {
			return false
		}
		keep[id] = true
		return true
	}
	for _, n := range g.Nodes {
		if n.Meta == "watch" {
			add(n.ID)
		}
	}
	for _, n := range g.Nodes {
		if n.Meta == "watch" {
			for _, nb := range adj[n.ID] {
				add(nb)
			}
		}
	}
	rest := append([]InvestigateGraphNode(nil), g.Nodes...)
	sort.SliceStable(rest, func(i, j int) bool {
		di, dj := degree[rest[i].ID], degree[rest[j].ID]
		if di != dj {
			return di > dj
		}
		if rest[i].Kind != rest[j].Kind {
			return rest[i].Kind == "campaign"
		}
		return rest[i].Label < rest[j].Label
	})
	for _, n := range rest {
		add(n.ID)
	}
	var nodes []InvestigateGraphNode
	for _, n := range g.Nodes {
		if keep[n.ID] {
			nodes = append(nodes, n)
		}
	}
	var links []InvestigateGraphLink
	for _, l := range g.Links {
		if keep[l.Source] && keep[l.Target] {
			links = append(links, l)
		}
	}
	if nodes == nil {
		nodes = []InvestigateGraphNode{}
	}
	if links == nil {
		links = []InvestigateGraphLink{}
	}
	return InvestigateGraph{Nodes: nodes, Links: links}
}

func investigateGraphJSON(g InvestigateGraph) string {
	if g.Nodes == nil {
		g.Nodes = []InvestigateGraphNode{}
	}
	if g.Links == nil {
		g.Links = []InvestigateGraphLink{}
	}
	b, err := json.Marshal(g)
	if err != nil {
		return `{"nodes":[],"links":[]}`
	}
	return string(b)
}
