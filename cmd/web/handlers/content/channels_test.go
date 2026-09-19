package content

import (
	"strings"
	"testing"

	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/osint"
)

func TestChannelView_CommentEngagement(t *testing.T) {
	html := renderInvestigate(t, templates.ChannelView(
		&db.GetChannelOverviewRow{Uploader: "Pat", VideoCount: 3},
		nil, "tester", nil, nil, false, nil, "",
		osint.ChannelEngagement{
			HarvestedComments: 100, OrganicComments: 60, CampaignComments: 30, SockComments: 20,
			UniqueCommenters: 40, UniqueOrganic: 28, SockCommenters: 8,
			Videos: 3, VideosCommented: 2, ViewsCommented: 10000, PlatformComments: 120,
			HarvestedPer1k: 10, OrganicPer1k: 6, Inflation: 0.4,
		},
	))
	for _, want := range []string{"Comment engagement", "organic comments", "inflation", "Investigate desk", "observations, not proof"} {
		if !strings.Contains(html, want) {
			t.Errorf("expected %q", want)
		}
	}
	if strings.Contains(html, "Approve") || strings.Contains(html, "Ban") {
		t.Error("channel engagement must not add moderation chips")
	}
}

func TestChannelView_NoHarvestedComments(t *testing.T) {
	html := renderInvestigate(t, templates.ChannelView(
		&db.GetChannelOverviewRow{Uploader: "Pat"},
		nil, "tester", nil, nil, false, nil, "",
		osint.ChannelEngagement{},
	))
	if !strings.Contains(html, "No harvested comments yet") {
		t.Fatal("expected empty engagement copy")
	}
}
