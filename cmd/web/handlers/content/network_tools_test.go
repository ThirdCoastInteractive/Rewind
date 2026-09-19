package content

import (
	"testing"

	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/wiki"
)

func TestNetworkVaultHome(t *testing.T) {
	members := []*db.ListNetworkChannelsRow{{Uploader: "LemonHedz", CreatorName: "Ben Avery"}}
	tree, slug := networkVaultHome("ch-1", "LemonHedz", "2dbdb2c1-ca02-4f9b-80fe-c0e5b6ec2ea8", members)
	if tree != wiki.TreeCreator || slug != "ben-avery" {
		t.Fatalf("channel with creator: %s/%s", tree, slug)
	}
	tree, slug = networkVaultHome("creator:x", "Devan Costa", "x", nil)
	if tree != wiki.TreeCreator || slug != "devan-costa" {
		t.Fatalf("wiki-only creator: %s/%s", tree, slug)
	}
}

func TestNetworkXHandle(t *testing.T) {
	for _, input := range []string{"@Old_Name", "https://x.com/Old_Name", "https://twitter.com/Old_Name/", "x.com/Old_Name"} {
		got, err := networkXHandle(input)
		if err != nil || got != "old_name" {
			t.Fatalf("%q => %q, %v", input, got, err)
		}
	}
	for _, input := range []string{"https://evilx.com/foo", "https://x.com/foo/status/123", "https://x.com/home", "https://x.com/i/user/123", "@bad-name", "https://x.com@evil.com/foo", "https://x.com:443/foo", "", "abcdefghijklmnop"} {
		if _, err := networkXHandle(input); err == nil {
			t.Errorf("accepted non-profile %q", input)
		}
	}
}
