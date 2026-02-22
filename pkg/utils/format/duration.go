package format

import "fmt"

// Duration converts seconds to "M:SS" or "H:MM:SS" display format.
func Duration(seconds float64) string {
	if seconds < 0 {
		return "0:00"
	}
	s := int(seconds)
	h := s / 3600
	m := (s % 3600) / 60
	sec := s % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, sec)
	}
	return fmt.Sprintf("%d:%02d", m, sec)
}

// DurationPtr formats a nullable int32 duration. Returns "" for nil.
func DurationPtr(seconds *int32) string {
	if seconds == nil {
		return ""
	}
	s := int(*seconds)
	if s < 0 {
		return ""
	}
	return Duration(float64(s))
}
