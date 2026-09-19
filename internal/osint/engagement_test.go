package osint

import "testing"

func TestChannelEngagementFinalize(t *testing.T) {
	e := ChannelEngagement{
		HarvestedComments: 100,
		OrganicComments:   60,
		CampaignComments:  30,
		SockComments:      20,
		ViewsCommented:    10_000,
	}
	e.Finalize()
	if e.HarvestedPer1k != 10 {
		t.Fatalf("harvested/1k=%v want 10", e.HarvestedPer1k)
	}
	if e.OrganicPer1k != 6 {
		t.Fatalf("organic/1k=%v want 6", e.OrganicPer1k)
	}
	if e.Inflation < 0.399 || e.Inflation > 0.401 {
		t.Fatalf("inflation=%v want 0.4", e.Inflation)
	}
}

func TestChannelEngagementFinalizeEmpty(t *testing.T) {
	var e ChannelEngagement
	e.Finalize()
	if e.HarvestedPer1k != 0 || e.OrganicPer1k != 0 || e.Inflation != 0 {
		t.Fatalf("%#v", e)
	}
}

func TestOrganicDoesNotGoNegative(t *testing.T) {
	if Organic(10, 8, 5) != 0 {
		t.Fatal("overlap should clamp at 0")
	}
	if Organic(10, 2, 3) != 5 {
		t.Fatal("organic=harvested-campaign-sock")
	}
}
