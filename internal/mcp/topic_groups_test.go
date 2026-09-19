package mcp

import "testing"

func TestTopicGroupsRequireBothTopicsWithASRAlternatives(t *testing.T) {
	groups, flat, _, err := compileTopicGroups([][]string{{"Callen", "Callan"}, {"Tesla", `"test lid"`}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		text string
		want bool
	}{{"Callan bought a Tesla", true}, {"Callen said test lid", true}, {"Callan talks about how old someone is", false}, {"Tesla test", false}} {
		if got := matchesTopicGroups(tc.text, groups, flat); got != tc.want {
			t.Errorf("%q: got %v", tc.text, got)
		}
	}
}
