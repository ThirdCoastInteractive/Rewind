package templates

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
)

func TestAttachWikiToGraphAddsVaultCreatorAndLink(t *testing.T) {
	ben := mustUUID("2dbdb2c1-ca02-4f9b-80fe-c0e5b6ec2ea8")
	devan := mustUUID("67749266-4518-4e84-be8b-f58ec230b5dd")
	graph := networkGraph{
		Nodes: []networkNode{{ID: "ch-ben", Label: "LemonHedz", CreatorID: ben.String(), CreatorName: "Ben Avery"}},
	}
	pages := []*db.WikiPage{
		{Tree: "creator", Slug: "ben-avery", Title: "Ben Avery", CreatorID: ben},
		{Tree: "creator", Slug: "devan-costa", Title: "Devan Costa", CreatorID: devan},
	}
	links := []*db.WikiLink{{FromTree: "creator", FromSlug: "ben-avery", ToTree: "creator", ToSlug: "devan-costa"}}
	attachWikiToGraph(&graph, pages, links, nil, nil)

	if len(graph.Nodes) != 2 {
		t.Fatalf("nodes=%d want 2 (existing channel + wiki-only Devan)", len(graph.Nodes))
	}
	var found bool
	for _, n := range graph.Nodes {
		if n.ID == "creator:"+devan.String() {
			found = true
			if n.Platform != "vault" || n.CreatorName != "Devan Costa" {
				t.Fatalf("vault node: %+v", n)
			}
		}
		if n.ID == "ch-ben" && n.Search == "" {
			t.Fatal("expected Ben channel search to include vault titles")
		}
	}
	if !found {
		t.Fatal("missing wiki-only creator node")
	}
	if len(graph.Links) != 1 || graph.Links[0].Kind != "wiki" {
		t.Fatalf("links=%v", graph.Links)
	}
	if graph.Links[0].Source != "creator:"+ben.String() || graph.Links[0].Target != "creator:"+devan.String() {
		t.Fatalf("wiki endpoints: %+v", graph.Links[0])
	}
}

func TestAttachWikiToGraphSkipsSelfAndUnattached(t *testing.T) {
	ben := mustUUID("2dbdb2c1-ca02-4f9b-80fe-c0e5b6ec2ea8")
	graph := networkGraph{Nodes: []networkNode{{ID: "ch-ben", CreatorID: ben.String()}}}
	pages := []*db.WikiPage{
		{Tree: "creator", Slug: "ben-avery", Title: "Ben Avery", CreatorID: ben},
		{Tree: "clipping", Slug: "ben-avery", Title: "The Show", CreatorID: ben},
		{Tree: "topic", Slug: "lemon-party", Title: "Lemon Party"},
	}
	links := []*db.WikiLink{
		{FromTree: "creator", FromSlug: "ben-avery", ToTree: "clipping", ToSlug: "ben-avery"},
		{FromTree: "topic", FromSlug: "lemon-party", ToTree: "creator", ToSlug: "ben-avery"},
	}
	attachWikiToGraph(&graph, pages, links, nil, nil)
	if len(graph.Links) != 0 {
		t.Fatalf("expected no graph edges, got %+v", graph.Links)
	}
}

func TestAttachWikiToGraphBundleKeepsLinkedNeighbor(t *testing.T) {
	ben := mustUUID("2dbdb2c1-ca02-4f9b-80fe-c0e5b6ec2ea8")
	jace := mustUUID("ffa633ab-f81c-4d9a-a9cc-1fa0cf8a1a43")
	graph := networkGraph{Nodes: []networkNode{{ID: "ch-ben", CreatorID: ben.String()}}}
	pages := []*db.WikiPage{
		{Tree: "creator", Slug: "ben-avery", Title: "Ben Avery", CreatorID: ben},
		{Tree: "creator", Slug: "jace-avery", Title: "Jace Avery", CreatorID: jace},
	}
	links := []*db.WikiLink{{FromTree: "creator", FromSlug: "ben-avery", ToTree: "creator", ToSlug: "jace-avery"}}
	attachWikiToGraph(&graph, pages, links, map[string]bool{ben.String(): true}, map[string]bool{"ch-ben": true})
	if len(graph.Nodes) != 2 {
		t.Fatalf("bundle should keep Jace as a wiki neighbor, nodes=%d", len(graph.Nodes))
	}
}

func mustUUID(s string) pgtype.UUID {
	var id pgtype.UUID
	u := uuid.MustParse(s)
	_ = id.Scan(u.String())
	return id
}
