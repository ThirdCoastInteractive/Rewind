package search

import (
	"reflect"
	"testing"
)

func TestParseLibrary(t *testing.T) {
	for _, tt := range []struct {
		query, general string
		fields         []FieldClause
	}{
		{`redbar interview`, `redbar interview`, nil},
		{`intranscript:Redbar is watching`, ``, []FieldClause{{"transcript", "Redbar is watching"}}},
		{`intitle:"interview" intranscript:"Redbar is watching"`, ``, []FieldClause{{"title", "interview"}, {"transcript", "Redbar is watching"}}},
		{`comedy intitle:"interview" redbar`, `comedy redbar`, []FieldClause{{"title", "interview"}}},
		{`INURL:https://youtube.com/watch?v=abc incontext:stand up comedy`, ``, []FieldClause{{"url", "https://youtube.com/watch?v=abc"}, {"context", "stand up comedy"}}},
		{`"intranscript:literal text"`, `"intranscript:literal text"`, nil},
		{`intitle:"" intranscript:`, ``, []FieldClause{{"title", ""}, {"transcript", ""}}},
		{`intitle:"unfinished`, ``, []FieldClause{{"title", "unfinished"}}},
		{`intitle:one intitle:two`, ``, []FieldClause{{"title", "one"}, {"title", "two"}}},
		{`unknown:word`, `unknown:word`, nil},
	} {
		t.Run(tt.query, func(t *testing.T) {
			got := ParseLibrary(tt.query)
			if got.General.Raw != tt.general || !reflect.DeepEqual(got.Fields, tt.fields) {
				t.Fatalf("got general=%q fields=%+v", got.General.Raw, got.Fields)
			}
		})
	}
}
