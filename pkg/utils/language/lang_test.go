package language

import (
	"database/sql/driver"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/text/language"
)

func TestTag_Scan_and_Value(t *testing.T) {
	var t1 Tag
	if err := t1.Scan("en-US"); err != nil {
		t.Fatalf("Scan error = %v", err)
	}
	if language.Tag(t1).String() != "en-US" {
		t.Fatalf("tag = %q, want %q", language.Tag(t1).String(), "en-US")
	}

	val, err := t1.Value()
	if err != nil {
		t.Fatalf("Value error = %v", err)
	}
	if s, ok := val.(string); !ok || s != "en-US" {
		t.Fatalf("Value() = %#v, want string(en-US)", val)
	}

	// nil scan -> Und
	var t2 Tag
	if err := t2.Scan(nil); err != nil {
		t.Fatalf("Scan(nil) error = %v", err)
	}
	if t2 != Tag(language.Und) {
		t.Fatalf("Scan(nil) got %q, want %q", language.Tag(t2).String(), language.Und.String())
	}

	// compile-time interface check
	var _ driver.Valuer = Tag(language.Und)
}

func TestTag_ScanText_and_TextValue(t *testing.T) {
	var t1 Tag
	if err := t1.ScanText(pgtype.Text{String: "fr", Valid: true}); err != nil {
		t.Fatalf("ScanText error = %v", err)
	}
	if language.Tag(t1).String() != "fr" {
		t.Fatalf("ScanText got %q, want %q", language.Tag(t1).String(), "fr")
	}

	text, err := t1.TextValue()
	if err != nil {
		t.Fatalf("TextValue error = %v", err)
	}
	if !text.Valid || text.String != "fr" {
		t.Fatalf("TextValue() = %#v, want Valid=true String=fr", text)
	}

	var t2 Tag
	if err := t2.ScanText(pgtype.Text{Valid: false}); err != nil {
		t.Fatalf("ScanText invalid error = %v", err)
	}
	if t2 != Tag(language.Und) {
		t.Fatalf("ScanText invalid got %q, want %q", language.Tag(t2).String(), language.Und.String())
	}
}
