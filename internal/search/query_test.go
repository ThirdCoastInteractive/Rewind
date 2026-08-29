package search

import "testing"

func TestCompileEmpty(t *testing.T) {
	if Compile("  ").TSQuery != "" {
		t.Fatal("empty query should compile to nothing")
	}
}

func TestCompilePrefix(t *testing.T) {
	got := Compile("lex fri")
	want := "lex & fri:*"
	if got.TSQuery != want {
		t.Fatalf("TSQuery = %q, want %q", got.TSQuery, want)
	}
}

func TestCompilePhrase(t *testing.T) {
	got := Compile(`"joe rogan" experience`)
	if got.TSQuery != "(joe <-> rogan) & experience:*" {
		t.Fatalf("TSQuery = %q", got.TSQuery)
	}
}

func TestCompileStripsOperators(t *testing.T) {
	got := Compile("foo & bar|baz")
	if got.TSQuery != "foo & barbaz:*" {
		t.Fatalf("TSQuery = %q, want %q", got.TSQuery, "foo & barbaz:*")
	}
}

func TestCompileSingleTokenPrefix(t *testing.T) {
	got := Compile("rogan")
	if got.TSQuery != "rogan:*" {
		t.Fatalf("TSQuery = %q", got.TSQuery)
	}
}
