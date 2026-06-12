// Package commentfmt provides helpers for rendering video comment text:
// linkifying inline timestamps (so they can seek the player) and safely
// rendering ts_headline search highlights.
package commentfmt

import (
	"html"
	"regexp"
	"strconv"
	"strings"
)

// Seg is one segment of comment text. When IsTime is true, Text is a timestamp
// label (e.g. "1:23") and Seconds is its position in the video for seeking.
type Seg struct {
	Text    string
	Seconds float64
	IsTime  bool
}

// tsRe matches M:SS, MM:SS, or H:MM:SS. The seconds (and minutes, when an hour
// is present) groups are exactly two digits, which avoids matching ratios like
// "16:9" or "2:1".
var tsRe = regexp.MustCompile(`\d{1,2}:\d{2}(?::\d{2})?`)

// ParseSegments splits comment text into plain and timestamp segments, in
// order. Matches that aren't valid times (e.g. "99:99") are kept as plain text.
// Returns nil for empty input.
func ParseSegments(text string) []Seg {
	if text == "" {
		return nil
	}
	locs := tsRe.FindAllStringIndex(text, -1)
	if len(locs) == 0 {
		return []Seg{{Text: text}}
	}
	segs := make([]Seg, 0, len(locs)*2+1)
	last := 0
	for _, loc := range locs {
		start, end := loc[0], loc[1]
		secs, ok := parseTimestamp(text[start:end])
		if !ok {
			// Leave this span as plain text: it gets absorbed by the next
			// plain segment (or the trailing remainder) below.
			continue
		}
		if start > last {
			segs = append(segs, Seg{Text: text[last:start]})
		}
		segs = append(segs, Seg{Text: text[start:end], Seconds: secs, IsTime: true})
		last = end
	}
	if last < len(text) {
		segs = append(segs, Seg{Text: text[last:]})
	}
	if len(segs) == 0 {
		return []Seg{{Text: text}}
	}
	return segs
}

// parseTimestamp converts a "M:SS" / "MM:SS" / "H:MM:SS" label to seconds.
func parseTimestamp(s string) (float64, bool) {
	parts := strings.Split(s, ":")
	var h, m, sec int
	var err error
	switch len(parts) {
	case 2:
		if m, err = strconv.Atoi(parts[0]); err != nil {
			return 0, false
		}
		if sec, err = strconv.Atoi(parts[1]); err != nil {
			return 0, false
		}
		if sec >= 60 {
			return 0, false
		}
	case 3:
		if h, err = strconv.Atoi(parts[0]); err != nil {
			return 0, false
		}
		if m, err = strconv.Atoi(parts[1]); err != nil {
			return 0, false
		}
		if sec, err = strconv.Atoi(parts[2]); err != nil {
			return 0, false
		}
		if m >= 60 || sec >= 60 {
			return 0, false
		}
	default:
		return 0, false
	}
	return float64(h*3600 + m*60 + sec), true
}

// Highlight sentinels emitted by ts_headline in SearchVideoComments (chr(2) and
// chr(3) — control chars that never appear in real comment text).
const (
	hlStart = "\x02"
	hlStop  = "\x03"
)

// SafeHighlight turns a ts_headline result (with chr(2)/chr(3) sentinels around
// matches) into safe HTML: the text is HTML-escaped first, then the sentinels
// are replaced with <mark>/</mark>. The result is safe to render raw.
func SafeHighlight(headline string) string {
	escaped := html.EscapeString(headline)
	escaped = strings.ReplaceAll(escaped, hlStart, "<mark>")
	escaped = strings.ReplaceAll(escaped, hlStop, "</mark>")
	return escaped
}
