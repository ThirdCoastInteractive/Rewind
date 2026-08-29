package cronspec

import (
	"testing"
	"time"
)

func TestValidate(t *testing.T) {
	valid := []string{
		"@hourly",
		"@daily",
		"@weekly",
		"@every 15m",
		"0 * * * *",
		"*/30 * * * *",
		"0 6 * * 1",
	}
	for _, spec := range valid {
		if err := Validate(spec); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", spec, err)
		}
	}

	invalid := []string{
		"",
		"   ",
		"not a cron",
		"* * * *",       // 4 fields
		"1 2 3 4 5 6",   // 6 fields (seconds not supported by standard parser)
		"* * * * *",     // every minute: below MinInterval
		"@every 30s",    // below MinInterval
		"*/5 * * * *",   // every 5 minutes: below MinInterval
	}
	for _, spec := range invalid {
		if err := Validate(spec); err == nil {
			t.Errorf("Validate(%q) = nil, want error", spec)
		}
	}
}

func TestNext(t *testing.T) {
	after := time.Date(2026, 7, 22, 10, 30, 0, 0, time.UTC)
	next, err := Next("0 * * * *", after)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	want := time.Date(2026, 7, 22, 11, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("Next = %v, want %v", next, want)
	}

	if _, err := Next("garbage", after); err == nil {
		t.Fatalf("expected error for invalid spec")
	}
}
