package templates

import (
	"encoding/json"
	"testing"
)

func TestAttachCommentersToGraphNodeType(t *testing.T) {
	graph := networkGraph{
		Nodes: []networkNode{{ID: "ch-1", Label: "Channel One"}},
	}
	attachCommentersToGraph(&graph, []NetworkCommenter{{
		ID:           "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		DisplayName:  "Sock Puppet",
		Source:       "youtube.com",
		CommentCount: 12,
		Watchlisted:  true,
		OpenFlagKinds: []string{"raid"},
	}}, []NetworkCommenterEdge{{
		ID:            "edge-1",
		FromChannelID: "ch-1",
		CommenterID:   "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		Kind:          "commented_by",
		Weight:        4,
		Evidence:      "commented_by",
	}})

	var commenter *networkNode
	for i := range graph.Nodes {
		if graph.Nodes[i].Type == "commenter" {
			commenter = &graph.Nodes[i]
			break
		}
	}
	if commenter == nil {
		t.Fatal("expected commenter node")
	}
	if commenter.ID != "commenter:aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" {
		t.Fatalf("id=%s", commenter.ID)
	}
	if commenter.Label != "Sock Puppet" || commenter.CommentCount != 12 || !commenter.Watchlisted {
		t.Fatalf("node=%+v", commenter)
	}
	if len(graph.Links) != 1 || graph.Links[0].Kind != "commented_by" {
		t.Fatalf("links=%+v", graph.Links)
	}
	raw, err := json.Marshal(commenter)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["type"] != "commenter" {
		t.Fatalf("json type=%v", decoded["type"])
	}
}

func TestNetworkCommentersDefaultOn(t *testing.T) {
	links := []networkLink{
		{Source: "ch-1", Target: "commenter:a", Kind: "commented_by", Weight: 2},
		{Source: "ch-1", Target: "ch-2", Kind: "outlink", Weight: 1},
	}
	if networkCommentersDefaultOn(links, "") {
		t.Fatal("empty focus defaults off")
	}
	if networkCommentersDefaultOn(links, "ch-2") {
		t.Fatal("focus without commenter neighbors defaults off")
	}
	if !networkCommentersDefaultOn(links, "ch-1") {
		t.Fatal("focus with commenter neighbor defaults on")
	}
	if !networkCommentersDefaultOn(links, "commenter:a") {
		t.Fatal("focused commenter defaults on")
	}
}

func TestAttachCommentersSkipsMissingChannelEndpoint(t *testing.T) {
	graph := networkGraph{}
	attachCommentersToGraph(&graph, []NetworkCommenter{{ID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", DisplayName: "X"}}, []NetworkCommenterEdge{{
		FromChannelID: "missing-channel",
		CommenterID:   "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		Kind:          "commented_by",
		Weight:        1,
	}})
	if len(graph.Nodes) != 1 {
		t.Fatalf("nodes=%d", len(graph.Nodes))
	}
	if len(graph.Links) != 0 {
		t.Fatalf("expected no edge to missing channel, got %+v", graph.Links)
	}
}
