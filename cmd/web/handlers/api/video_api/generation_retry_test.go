package video_api

import "testing"

func TestParseRepairTime(t *testing.T) {
	for raw, want := range map[string]float64{"36:00": 2160, "00:57:00": 3420, "42.5": 42.5} {
		got, err := parseRepairTime(raw)
		if err != nil || got != want {
			t.Fatalf("%s = %v, %v", raw, got, err)
		}
	}
	for _, raw := range []string{"", "NaN", "Inf", "-1", "00:61:00", "1:2:3:4"} {
		if _, err := parseRepairTime(raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
