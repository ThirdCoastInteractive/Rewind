// Package channellinks harvests directed channel edges from video text
// (title, description) and optional comment-author profile URLs.
package channellinks

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Edge kinds stored on channel_edges.kind.
const (
	KindOutlink   = "outlink"   // youtube.com/@, /channel/UC, rumble.com/c/, kick.com/
	KindMention   = "mention"   // @handle that looks like a channel
	KindCommented = "commented" // comment author_url matches another archived channel
)

// Hit is one extracted channel reference in source text.
type Hit struct {
	URL      string
	Kind     string
	Evidence string
}

var (
	// urlLikeRe finds http(s) URLs and scheme-less host/path tokens for the
	// platforms we harvest. A second pass classifies the path.
	urlLikeRe = regexp.MustCompile(`(?i)\b(?:https?://)?(?:www\.)?(?:youtube\.com|youtu\.be|rumble\.com|kick\.com|twitter\.com|x\.com)/[^\s<>"'()]+`)
	mentionRe = regexp.MustCompile(`(?:^|[^A-Za-z0-9_./])@([A-Za-z0-9][A-Za-z0-9._-]{2,29})\b`)
)

var kickReserved = map[string]struct{}{
	"video": {}, "videos": {}, "categories": {}, "following": {},
	"browse": {}, "search": {}, "login": {}, "signup": {}, "api": {},
}

// Parse extracts outlink and mention hits from title+description text.
// Bare @handles default to Twitter/X; YouTube @handles are harvested as
// outlinks when the full youtube.com/@ URL is present.
func Parse(text string) []Hit {
	return ParseOn("", text)
}

// ParseOn is Parse with a source platform hint (youtube, twitter, other, …).
func ParseOn(sourcePlatform, text string) []Hit {
	if strings.TrimSpace(text) == "" {
		return nil
	}

	var hits []Hit
	seen := map[string]struct{}{}
	covered := make([]bool, len(text))

	for _, loc := range urlLikeRe.FindAllStringIndex(text, -1) {
		raw := text[loc[0]:loc[1]]
		raw = strings.TrimRight(raw, ".,;:!?)]}")
		abs, kind, ok := classifyOutlink(raw)
		if !ok {
			continue
		}
		key := kind + "\n" + abs
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		hits = append(hits, Hit{
			URL:      abs,
			Kind:     kind,
			Evidence: snippetAround(text, loc[0], loc[1], 120),
		})
		for i := loc[0]; i < loc[1] && i < len(covered); i++ {
			covered[i] = true
		}
	}

	for _, loc := range mentionRe.FindAllStringSubmatchIndex(text, -1) {
		if len(loc) < 4 {
			continue
		}
		start, end := loc[2], loc[3]
		if start < 0 || end > len(text) {
			continue
		}
		if spanCovered(covered, loc[0], loc[1]) {
			continue
		}
		handle := text[start:end]
		abs := mentionURL(sourcePlatform, handle)
		key := KindMention + "\n" + strings.ToLower(abs)
		if _, dup := seen[key]; dup {
			continue
		}
		ytOut := KindOutlink + "\nhttps://youtube.com/@" + handle
		twOut := KindOutlink + "\nhttps://x.com/" + handle
		if _, dup := seen[ytOut]; dup {
			continue
		}
		if _, dup := seen[twOut]; dup {
			continue
		}
		seen[key] = struct{}{}
		hits = append(hits, Hit{
			URL:      abs,
			Kind:     KindMention,
			Evidence: snippetAround(text, loc[0], loc[1], 120),
		})
	}

	return hits
}

func mentionURL(sourcePlatform, handle string) string {
	handle = strings.TrimPrefix(strings.TrimSpace(handle), "@")
	if handle == "" {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(sourcePlatform)) {
	case "youtube":
		return "https://youtube.com/@" + handle
	case "kick":
		return "https://kick.com/" + handle
	default:
		// Twitter/X, "other" (usually tweets), and unknown: @handle is a tweet mention.
		return "https://x.com/" + handle
	}
}

func spanCovered(covered []bool, start, end int) bool {
	if start < 0 {
		start = 0
	}
	if end > len(covered) {
		end = len(covered)
	}
	for i := start; i < end; i++ {
		if covered[i] {
			return true
		}
	}
	return false
}

func classifyOutlink(raw string) (abs string, kind string, ok bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", "", false
	}
	if !strings.Contains(s, "://") {
		s = "https://" + strings.TrimPrefix(s, "//")
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", "", false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 1 && parts[0] == "" {
		parts = nil
	}

	switch host {
	case "youtube.com", "m.youtube.com", "music.youtube.com":
		if len(parts) >= 1 && strings.HasPrefix(parts[0], "@") {
			handle := parts[0]
			return "https://youtube.com/" + handle, KindOutlink, true
		}
		if len(parts) >= 2 && strings.EqualFold(parts[0], "channel") && strings.HasPrefix(parts[1], "UC") {
			return "https://youtube.com/channel/" + parts[1], KindOutlink, true
		}
		return "", "", false
	case "rumble.com":
		if len(parts) >= 2 && strings.EqualFold(parts[0], "c") && parts[1] != "" {
			return "https://rumble.com/c/" + parts[1], KindOutlink, true
		}
		return "", "", false
	case "twitter.com", "x.com", "mobile.twitter.com":
		if len(parts) >= 1 && parts[0] != "" {
			h := strings.TrimPrefix(parts[0], "@")
			switch strings.ToLower(h) {
			case "home", "explore", "search", "i", "intent", "share", "hashtag", "settings", "compose":
				return "", "", false
			}
			if h != "" {
				return "https://x.com/" + h, KindOutlink, true
			}
		}
		return "", "", false
	case "kick.com":
		if len(parts) >= 1 && parts[0] != "" {
			seg := strings.ToLower(parts[0])
			if _, reserved := kickReserved[seg]; reserved {
				return "", "", false
			}
			return "https://kick.com/" + parts[0], KindOutlink, true
		}
		return "", "", false
	default:
		return "", "", false
	}
}

func snippetAround(text string, start, end, max int) string {
	if start < 0 {
		start = 0
	}
	if end > len(text) {
		end = len(text)
	}
	pad := 40
	from := start - pad
	if from < 0 {
		from = 0
	}
	to := end + pad
	if to > len(text) {
		to = len(text)
	}
	// Snap to rune boundaries so we don't cut a multi-byte character.
	for from < len(text) && !utf8.RuneStart(text[from]) {
		from++
	}
	for to > 0 && to < len(text) && !utf8.RuneStart(text[to]) {
		to--
	}
	s := strings.Join(strings.FieldsFunc(text[from:to], func(r rune) bool {
		return unicode.IsSpace(r)
	}), " ")
	runes := []rune(s)
	if len(runes) > max {
		s = string(runes[:max]) + "…"
	}
	return s
}
