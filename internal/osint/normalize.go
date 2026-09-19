package osint

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	urlRe     = regexp.MustCompile(`(?i)\bhttps?://\S+|\bwww\.\S+`)
	mentionRe = regexp.MustCompile(`(^|\s)@[A-Za-z0-9_.-]+`)
)

// NormalizeText lowercases, strips URLs and @mentions, and collapses whitespace.
func NormalizeText(s string) string {
	s = strings.ToLower(s)
	s = urlRe.ReplaceAllString(s, " ")
	s = mentionRe.ReplaceAllString(s, " ")
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := true
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return strings.TrimSpace(b.String())
}
