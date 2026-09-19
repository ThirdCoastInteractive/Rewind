package contextwindow

import "testing"

func TestValidateDurationBounds(t *testing.T) {
	d := int32(10)
	if err := Validate(0, 5, "ok", &d); err != nil {
		t.Fatal(err)
	}
	if err := Validate(0, 11, "ok", &d); err == nil {
		t.Fatal("expected duration overflow")
	}
	if err := Validate(0, 5, "  ", &d); err == nil {
		t.Fatal("expected empty title")
	}
	if err := Validate(5, 5, "ok", &d); err == nil {
		t.Fatal("expected empty range")
	}
}
