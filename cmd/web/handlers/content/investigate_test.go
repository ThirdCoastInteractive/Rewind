package content

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/cmd/web/templates/components"
)

func renderInvestigate(t *testing.T, comp templ.Component) string {
	t.Helper()
	var b bytes.Buffer
	if err := comp.Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

func TestCommentRowDossierLink(t *testing.T) {
	html := renderInvestigate(t, components.CommentRow(components.CommentItem{
		Author: "alice", CommenterID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", Text: "hi",
	}))
	if !strings.Contains(html, "/investigate/commenters/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa") {
		t.Fatal("expected dossier href")
	}
	for _, ban := range []string{"Approve", "Ban"} {
		if strings.Contains(html, ban) {
			t.Errorf("comment row must not contain %q", ban)
		}
	}
}

func TestInvestigateIndex_EmptyStates(t *testing.T) {
	html := renderInvestigate(t, templates.Investigate(templates.InvestigateIndexData{
		Username: "tester",
	}))
	for _, want := range []string{"No watchlist", "No open flags"} {
		if !strings.Contains(html, want) {
			t.Errorf("expected %q in output", want)
		}
	}
	for _, ban := range []string{"Approve", "Reject", "Ban", "Hold", "Hide comment"} {
		if strings.Contains(html, ban) {
			t.Errorf("investigate desk must not contain moderation chip %q", ban)
		}
	}
	if !strings.Contains(html, "observations, not proof") {
		t.Error("expected observation caveat")
	}
	if !strings.Contains(html, "Index X replies") || !strings.Contains(html, "/investigate/x-replies") {
		t.Error("expected on-demand X reply index form")
	}
	if !strings.Contains(html, "data-investigate-graph") || !strings.Contains(html, "investigate-graph.js") {
		t.Error("expected graph mount and script")
	}
	if !strings.Contains(html, "No actors yet") {
		t.Error("expected empty graph copy")
	}
}

func TestInvestigateIndex_GraphAndCards(t *testing.T) {
	html := renderInvestigate(t, templates.Investigate(templates.InvestigateIndexData{
		Username: "tester",
		Watchlist: []templates.InvestigateWatchItem{{
			CommenterID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
			DisplayName: "Pat",
			AuthorID:    "UC1",
			Source:      "youtube.com",
		}},
		Flags: []templates.InvestigateFlagItem{{
			ID: "ffffffff-ffff-ffff-ffff-ffffffffffff", Kind: "raid",
			CommenterID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", CommenterName: "Pat",
			CampaignID: "cccccccc-cccc-cccc-cccc-cccccccccccc",
		}},
		Campaigns: []templates.InvestigateCampaignItem{{
			ID: "cccccccc-cccc-cccc-cccc-cccccccccccc", Kind: "raid", NormalizedText: "copy this",
		}},
		Graph: templates.BuildInvestigateGraph(
			[]templates.InvestigateWatchItem{{CommenterID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", DisplayName: "Pat"}},
			[]templates.InvestigateFlagItem{{Kind: "raid", CommenterID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", CampaignID: "cccccccc-cccc-cccc-cccc-cccccccccccc"}},
			[]templates.InvestigateCampaignItem{{ID: "cccccccc-cccc-cccc-cccc-cccccccccccc", Kind: "raid", NormalizedText: "copy this"}},
			[]templates.InvestigateGraphEdge{{From: "commenter:aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", To: "commenter:bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", Kind: "style", ToLabel: "other"}},
		),
	}))
	for _, want := range []string{
		"entity-card-grid", "Pat", "copy this", "Dismiss",
		"commenter:aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		"campaign:cccccccc-cccc-cccc-cccc-cccccccccccc",
		"investigate-graph.js",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("expected %q", want)
		}
	}
	for _, ban := range []string{"Approve", "Reject", "Ban"} {
		if strings.Contains(html, ban) {
			t.Errorf("must not contain %q", ban)
		}
	}
}

func TestInvestigateCommenter_NoModerationChips(t *testing.T) {
	html := renderInvestigate(t, templates.InvestigateCommenter(templates.CommenterDossier{
		ID:          "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		DisplayName: "sock",
		AuthorID:    "UC123",
	}, "tester"))
	for _, ban := range []string{"Approve", "Reject", "Ban", "Hold", "Hide comment"} {
		if strings.Contains(html, ban) {
			t.Errorf("dossier must not contain %q", ban)
		}
	}
	if !strings.Contains(html, "observations, not proof") {
		t.Error("expected observation caveat")
	}
	if !strings.Contains(html, "investigate-graph.js") || !strings.Contains(html, "Neighborhood") {
		t.Error("expected neighborhood graph on dossier")
	}
}

func TestInvestigateCampaign_CardsAndGraph(t *testing.T) {
	html := renderInvestigate(t, templates.InvestigateCampaign(templates.InvestigateCampaignDetail{
		ID:             "cccccccc-cccc-cccc-cccc-cccccccccccc",
		Kind:           "raid",
		NormalizedText: "copy this",
		Members: []templates.CampaignMemberItem{{
			VideoID: "vvvvvvvv-vvvv-vvvv-vvvv-vvvvvvvvvvvv", Author: "Pat",
			CommenterID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", Text: "copy this",
		}},
		Graph: templates.BuildInvestigateGraph(
			[]templates.InvestigateWatchItem{{CommenterID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", DisplayName: "Pat"}},
			nil,
			[]templates.InvestigateCampaignItem{{ID: "cccccccc-cccc-cccc-cccc-cccccccccccc", Kind: "raid", NormalizedText: "copy this"}},
			[]templates.InvestigateGraphEdge{{From: "commenter:aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", To: "campaign:cccccccc-cccc-cccc-cccc-cccccccccccc", Kind: "campaign"}},
		),
	}, "tester"))
	for _, want := range []string{"copy this", "Pat", "entity-card-grid", "investigate-graph.js", "campaign:cccccccc-cccc-cccc-cccc-cccccccccccc"} {
		if !strings.Contains(html, want) {
			t.Errorf("expected %q", want)
		}
	}
	for _, ban := range []string{"Approve", "Reject", "Ban"} {
		if strings.Contains(html, ban) {
			t.Errorf("must not contain %q", ban)
		}
	}
}

