package search

import (
	"strings"
	"testing"
)

func TestMatchUsesCompiledSemantics(t *testing.T) {
	tests := []struct {
		query string
		text  string
		want  bool
	}{
		{"redbar", "They talked about Redbar yesterday.", true},
		{"joe rog", "Joe Rogan said it.", true},
		{"Joe? ?Rogan", "Joe Rogan said it.", true},
		{"\"joe rogan\"", "Joe, Rogan said it.", true},
		{"\"joe rogan\"", "Joe met Rogan later.", false},
		{"redbar kino", "Kino mentioned Redbar.", true},
		{"redbar kino", "Only Redbar appears.", false},
		{"redbar -kino", "Redbar talked about Mike.", true},
		{"redbar -kino", "Kino mentioned Redbar.", false},
		{"redbar -\"joe rogan\"", "Redbar discussed Joe Rogan.", false},
		{"redbar -\"joe rogan\"", "Redbar discussed Joe and later Rogan.", true},
		{"-redbar", "This is about Kino.", false},
		{"-redbar", "This is about Redbar Radio.", false},
	}
	for _, tt := range tests {
		if got := Compile(tt.query).Match(tt.text); got != tt.want {
			t.Errorf("Compile(%q).Match(%q) = %v, want %v", tt.query, tt.text, got, tt.want)
		}
	}
}

func TestCompileRejectsNegativeOnlyQuery(t *testing.T) {
	got := Compile(`-redbar`)
	if got.TSQuery != "" || got.HasPositive {
		t.Fatalf("negative-only query should not execute: %#v", got)
	}
}

func TestCompileNegation(t *testing.T) {
	got := Compile(`redbar -"joe rogan"`)
	if got.TSQuery != `redbar & !((joe <-> rogan))` {
		t.Fatalf("TSQuery = %q", got.TSQuery)
	}
	if !got.HasNegation {
		t.Fatal("expected HasNegation")
	}
}

func TestCompileTSQuery(t *testing.T) {
	tests := []struct {
		query      string
		contains   []string
		notContain []string
	}{
		{query: "redbar", contains: []string{"redbar"}},
		{query: `"joe rogan"`, contains: []string{"joe <-> rogan"}},
		{query: "Joe? ?Rogan", contains: []string{"joe", "rogan"}, notContain: []string{"?"}},
	}
	for _, tt := range tests {
		got := Compile(tt.query).TSQuery
		for _, want := range tt.contains {
			if !strings.Contains(got, want) {
				t.Errorf("Compile(%q).TSQuery = %q, want to contain %q", tt.query, got, want)
			}
		}
		for _, forbid := range tt.notContain {
			if strings.Contains(got, forbid) {
				t.Errorf("Compile(%q).TSQuery = %q, want no %q", tt.query, got, forbid)
			}
		}
	}
}
