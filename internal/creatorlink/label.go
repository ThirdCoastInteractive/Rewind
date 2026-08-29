package creatorlink

import (
	"regexp"
	"strings"
)

const (
	RoleMain = "main"
	RoleAlt  = "alt"
)

var (
	mainCue = regexp.MustCompile(`(?i)\b(main channel|primary channel|my (main|youtube) channel)\b`)
	altCue  = regexp.MustCompile(`(?i)\b(alt(ernate)? channel|second channel|clips? channel|vods? channel|backup channel|live channel|stream(ing)? channel|storm chasing channel)\b`)
)

// RoleFromEvidence classifies a harvested outlink snippet as pointing at a
// main or alt channel of the same person. Empty means no identity signal.
func RoleFromEvidence(evidence string) string {
	s := strings.TrimSpace(evidence)
	if s == "" {
		return ""
	}
	if mainCue.MatchString(s) {
		return RoleMain
	}
	if altCue.MatchString(s) {
		return RoleAlt
	}
	return ""
}

func handleName(uploader, url string) string {
	u := strings.TrimSpace(uploader)
	if u != "" && !strings.HasPrefix(u, "http") {
		return u
	}
	lower := strings.ToLower(url)
	for _, prefix := range []string{"youtube.com/@", "x.com/", "twitter.com/", "rumble.com/c/", "kick.com/"} {
		i := strings.Index(lower, prefix)
		if i < 0 {
			continue
		}
		rest := strings.Trim(url[i+len(prefix):], "/")
		if j := strings.IndexAny(rest, "/?#"); j >= 0 {
			rest = rest[:j]
		}
		rest = strings.TrimPrefix(rest, "@")
		if rest != "" {
			return rest
		}
	}
	return strings.TrimSpace(uploader)
}
