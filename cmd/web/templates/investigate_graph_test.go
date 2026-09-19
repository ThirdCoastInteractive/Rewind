package templates

import "testing"

func TestBuildInvestigateGraphCardsAndEdges(t *testing.T) {
	g := BuildInvestigateGraph(
		[]InvestigateWatchItem{{CommenterID: "aa", DisplayName: "Pat", AuthorID: "UC1"}},
		[]InvestigateFlagItem{{Kind: "raid", CommenterID: "aa", CommenterName: "Pat", CampaignID: "cc"}},
		[]InvestigateCampaignItem{{ID: "cc", Kind: "raid", NormalizedText: "copy this"}},
		[]InvestigateGraphEdge{
			{From: "commenter:aa", To: "commenter:bb", Kind: "style"},
			{From: "commenter:aa", To: "campaign:cc", Kind: "campaign"},
		},
	)
	if len(g.Nodes) != 3 {
		t.Fatalf("nodes=%d want 3", len(g.Nodes))
	}
	kinds := map[string]int{}
	for _, l := range g.Links {
		kinds[l.Kind]++
	}
	if kinds["raid"] != 1 || kinds["campaign"] != 1 || kinds["style"] != 1 {
		t.Fatalf("links %#v", g.Links)
	}
}

func TestBuildInvestigateGraphEmptySlices(t *testing.T) {
	g := BuildInvestigateGraph(nil, nil, nil, nil)
	if g.Nodes == nil || g.Links == nil {
		t.Fatalf("empty graph should marshal as arrays, got nodes=%v links=%v", g.Nodes, g.Links)
	}
	raw := investigateGraphJSON(g)
	if raw != `{"nodes":[],"links":[]}` {
		t.Fatalf("json=%s", raw)
	}
}

func TestBuildInvestigateGraphKeepsLabelsFromSeed(t *testing.T) {
	g := BuildInvestigateGraph(
		[]InvestigateWatchItem{{CommenterID: "aa", DisplayName: "Pat"}},
		nil,
		[]InvestigateCampaignItem{{ID: "cc", Kind: "raid", NormalizedText: "copy this"}},
		[]InvestigateGraphEdge{{From: "commenter:aa", To: "campaign:cc", Kind: "campaign"}},
	)
	labels := map[string]string{}
	for _, n := range g.Nodes {
		labels[n.ID] = n.Label
	}
	if labels["commenter:aa"] != "Pat" || labels["campaign:cc"] != "copy this" {
		t.Fatalf("labels %#v", labels)
	}
}

func TestEdgesFromFlagEvidenceSockSuggest(t *testing.T) {
	extra := EdgesFromFlagEvidence([]InvestigateFlagItem{{
		Kind: "sock_suggest", CommenterName: "Pat", CommenterID: "aa", PeerID: "bb",
	}})
	if len(extra) != 1 || extra[0].Kind != "style" || extra[0].From != "commenter:aa" || extra[0].To != "commenter:bb" {
		t.Fatalf("%#v", extra)
	}
}

func TestPruneInvestigateGraphKeepsWatchAndDegree(t *testing.T) {
	g := BuildInvestigateGraph(
		[]InvestigateWatchItem{{CommenterID: "watch", DisplayName: "Pin"}},
		nil, nil,
		[]InvestigateGraphEdge{
			{From: "commenter:watch", To: "commenter:near", Kind: "style", ToLabel: "Near"},
			{From: "commenter:far1", To: "commenter:far2", Kind: "style", FromLabel: "A", ToLabel: "B"},
			{From: "commenter:far1", To: "commenter:far3", Kind: "style", ToLabel: "C"},
			{From: "commenter:far2", To: "commenter:far3", Kind: "style"},
		},
	)
	got := PruneInvestigateGraph(g, 3)
	if len(got.Nodes) != 3 {
		t.Fatalf("nodes=%d want 3 labels=%v", len(got.Nodes), nodeLabels(got))
	}
	ids := map[string]bool{}
	for _, n := range got.Nodes {
		ids[n.ID] = true
	}
	if !ids["commenter:watch"] || !ids["commenter:near"] {
		t.Fatalf("expected watch neighborhood, got %v", nodeLabels(got))
	}
}

func nodeLabels(g InvestigateGraph) []string {
	var out []string
	for _, n := range g.Nodes {
		out = append(out, n.ID+":"+n.Label)
	}
	return out
}
