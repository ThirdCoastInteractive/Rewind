package format

import (
	"fmt"
	"time"
)

// FormatBytes returns a human-readable byte size (e.g. "1.5 MB").
func Bytes(b int64) string {
	const unit = 1024
	if b < unit {
		return Itoa64(b) + " B"
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// Number formats an int with K/M suffixes for display (e.g. 1500 → "1.5K").
func Number(n int) string {
	if n >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	} else if n >= 1000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

// Itoa formats an int as a string.
func Itoa(i int) string {
	return fmt.Sprintf("%d", i)
}

// Itoa32 formats an int32 as a string.
func Itoa32(i int32) string {
	return fmt.Sprintf("%d", i)
}

// Itoa64 formats an int64 as a string.
func Itoa64(i int64) string {
	return fmt.Sprintf("%d", i)
}

// Truncate returns s truncated to max characters with "..." suffix.
func Truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

// ToInt64 safely converts interface{} to int64.
func ToInt64(v interface{}) int64 {
	switch val := v.(type) {
	case int64:
		return val
	case int32:
		return int64(val)
	case int:
		return int64(val)
	case float64:
		return int64(val)
	default:
		return 0
	}
}

// JobDuration formats a time.Duration as a human-readable string
// (e.g. "3.2 seconds", "1.5 minutes", "2.0 hours").
func JobDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1f seconds", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%.1f minutes", d.Minutes())
	}
	return fmt.Sprintf("%.1f hours", d.Hours())
}
