package creatorlink

import "testing"

func TestRoleFromEvidence(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"x 901 Metamora, IL 61548 main channel: https://www.youtube.com/@styropyro storm", RoleMain},
		{"e.com/@styropyro storm chasing channel: https://www.youtube.com/@styro_drake ins", RoleAlt},
		{"check my clips channel for the rest", RoleAlt},
		{"collab with https://youtube.com/@someone today", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := RoleFromEvidence(tc.in); got != tc.want {
			t.Errorf("RoleFromEvidence(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}
