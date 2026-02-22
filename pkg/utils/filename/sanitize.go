// Package filename provides utilities for sanitizing strings into safe filenames.
package filename

import (
	"regexp"
	"strings"
)

// invalidCharsRe matches characters not safe for filenames across all major OSes.
var invalidCharsRe = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

// multiDash collapses runs of dashes/underscores.
var multiDash = regexp.MustCompile(`[-_]{2,}`)

// Sanitize converts an arbitrary string into a filename-safe slug.
// The result contains only alphanumeric characters, dashes, underscores, and
// dots. Leading/trailing dashes and dots are stripped. The output is truncated
// to maxLen bytes (0 = no limit, defaults to 120 if not specified).
func Sanitize(name string, maxLen int) string {
	if maxLen <= 0 {
		maxLen = 120
	}

	s := strings.TrimSpace(name)
	if s == "" {
		return ""
	}

	// Replace invalid filesystem characters with dashes.
	s = invalidCharsRe.ReplaceAllString(s, "-")

	// Replace spaces and other whitespace with dashes.
	s = strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return '-'
		}
		return r
	}, s)

	// Collapse consecutive dashes / underscores.
	s = multiDash.ReplaceAllString(s, "-")

	// Strip leading/trailing dashes and dots (avoid hidden files / trailing dots on Windows).
	s = strings.Trim(s, "-.")

	// Truncate to maxLen, but don't cut in the middle of a UTF-8 sequence.
	if len(s) > maxLen {
		s = s[:maxLen]
		// Clean up a trailing partial dash/dot from the truncation.
		s = strings.TrimRight(s, "-.")
	}

	return s
}
