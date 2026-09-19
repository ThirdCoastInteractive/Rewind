package comments

import (
	"regexp"
	"strings"
)

// youtubeChannelUC matches youtube.com/channel/UC… (optional www), case-insensitive.
var youtubeChannelUC = regexp.MustCompile(`(?i)(?:www\.)?youtube\.com/channel/(UC[A-Za-z0-9_-]+)`)

// CommenterKey mirrors SQL comment_author_id: nonempty author_id wins; else UC
// from a YouTube channel URL. Empty string when both are unusable.
func CommenterKey(authorID, authorURL string) string {
	if id := strings.TrimSpace(authorID); id != "" {
		return id
	}
	m := youtubeChannelUC.FindStringSubmatch(strings.TrimSpace(authorURL))
	if len(m) == 2 {
		return m[1]
	}
	return ""
}
