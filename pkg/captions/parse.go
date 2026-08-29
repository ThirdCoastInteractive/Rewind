// Package captions parses, cleans, and re-emits WebVTT subtitle files.
//
// YouTube auto-captions arrive with karaoke timing tags, cue settings, HTML
// entities, and a rolling two-line window that repeats every phrase. This
// package strips that markup and collapses duplicates so transcripts can be
// indexed and shown as readable cues.
package captions

import (
	"bufio"
	"html"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// Cue is one cleaned subtitle cue with times in seconds.
type Cue struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

// Document is a parsed, cleaned WebVTT.
type Document struct {
	Cues  []Cue
	Dirty bool // original had karaoke tags, cue settings, or entities worth rewriting
}

type rawCue struct {
	start, end float64
	lines      []string
	karaoke    bool
	settings   bool
}

var (
	karaokeRe    = regexp.MustCompile(`<\d{1,2}:\d{2}(?::\d{2})?(?:\.\d+)?>|<c[.\s>]|</c>`)
	tagRe        = regexp.MustCompile(`<[^>]*>`)
	cueSettingRe = regexp.MustCompile(`\b(align|position|line|size|vertical|region):`)
	headerKeyRe  = regexp.MustCompile(`^(Kind|Language|Style):`)
)

// Parse reads a WebVTT (or VTT-like) stream and returns cleaned cues.
func Parse(r io.Reader) (*Document, error) {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 16*1024*1024)

	var (
		cues       []rawCue
		inHeader   = true
		sawWEBVTT  bool
		dirty      bool
		pendingID  string
	)

	flushCue := func(start, end float64, settings bool, textLines []string) {
		if start < 0 || end < 0 {
			return
		}
		joined := strings.Join(textLines, "\n")
		karaoke := karaokeRe.MatchString(joined)
		if karaoke || settings {
			dirty = true
		}
		cues = append(cues, rawCue{start: start, end: end, lines: textLines, karaoke: karaoke, settings: settings})
	}

	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		trim := strings.TrimSpace(line)

		if inHeader {
			if !sawWEBVTT {
				if strings.HasPrefix(trim, "WEBVTT") {
					sawWEBVTT = true
					continue
				}
				// Tolerate files that skip the header.
				if strings.Contains(trim, "-->") {
					inHeader = false
				} else {
					if trim == "" {
						inHeader = false
					}
					continue
				}
			} else {
				if trim == "" {
					inHeader = false
					continue
				}
				if strings.EqualFold(trim, "NOTE") || strings.HasPrefix(strings.ToUpper(trim), "NOTE ") ||
					strings.EqualFold(trim, "STYLE") || headerKeyRe.MatchString(trim) {
					continue
				}
				if strings.Contains(trim, "-->") {
					inHeader = false
				} else {
					continue
				}
			}
		}

		if trim == "" {
			pendingID = ""
			continue
		}
		if strings.EqualFold(trim, "NOTE") || strings.HasPrefix(strings.ToUpper(trim), "NOTE ") ||
			strings.EqualFold(trim, "STYLE") {
			// Skip until blank; handled by empty-line continue on subsequent rows.
			for scanner.Scan() {
				if strings.TrimSpace(scanner.Text()) == "" {
					break
				}
			}
			pendingID = ""
			continue
		}
		if !strings.Contains(trim, "-->") {
			pendingID = trim
			_ = pendingID
			continue
		}

		start, end, settings := parseTimestampLine(trim)
		if settings {
			dirty = true
		}
		var textLines []string
		for scanner.Scan() {
			t := strings.TrimRight(scanner.Text(), "\r")
			if strings.TrimSpace(t) == "" {
				break
			}
			textLines = append(textLines, t)
		}
		flushCue(start, end, settings, textLines)
		pendingID = ""
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	cleaned := collapse(cues, &dirty)
	return &Document{Cues: cleaned, Dirty: dirty}, nil
}

// ParseString is Parse over a string.
func ParseString(s string) (*Document, error) {
	return Parse(strings.NewReader(s))
}

// PlainText concatenates cue text into a single searchable blob.
func PlainText(cues []Cue) string {
	parts := make([]string, 0, len(cues))
	for _, c := range cues {
		t := strings.TrimSpace(c.Text)
		if t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, " ")
}

func parseTimestampLine(line string) (start, end float64, settings bool) {
	parts := strings.SplitN(line, "-->", 2)
	if len(parts) != 2 {
		return -1, -1, false
	}
	startFields := strings.Fields(strings.TrimSpace(parts[0]))
	if len(startFields) == 0 {
		return -1, -1, false
	}
	start = parseVTTTime(startFields[0])
	rest := strings.TrimSpace(parts[1])
	endFields := strings.Fields(rest)
	if len(endFields) == 0 {
		return start, -1, false
	}
	end = parseVTTTime(endFields[0])
	if len(endFields) > 1 || cueSettingRe.MatchString(rest) {
		settings = true
	}
	return start, end, settings
}

func parseVTTTime(t string) float64 {
	t = strings.TrimSpace(t)
	// HH:MM:SS.mmm or MM:SS.mmm
	parts := strings.Split(t, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return -1
	}
	var hh, mm float64
	var ssPart string
	if len(parts) == 3 {
		h, err := strconv.ParseFloat(parts[0], 64)
		if err != nil {
			return -1
		}
		hh = h
		m, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			return -1
		}
		mm = m
		ssPart = parts[2]
	} else {
		m, err := strconv.ParseFloat(parts[0], 64)
		if err != nil {
			return -1
		}
		mm = m
		ssPart = parts[1]
	}
	ss, err := strconv.ParseFloat(ssPart, 64)
	if err != nil {
		return -1
	}
	return hh*3600 + mm*60 + ss
}

func stripCueText(s string) string {
	if karaokeRe.MatchString(s) {
		s = karaokeRe.ReplaceAllString(s, "")
	}
	s = tagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	// Collapse internal whitespace but keep it one line for index/display.
	fields := strings.Fields(s)
	return strings.TrimSpace(strings.Join(fields, " "))
}

func collapse(raw []rawCue, dirty *bool) []Cue {
	hadKaraoke := false
	for _, r := range raw {
		if r.karaoke {
			hadKaraoke = true
			break
		}
	}

	out := make([]Cue, 0, len(raw))
	for _, r := range raw {
		if hadKaraoke && r.karaoke {
			continue
		}
		text := stripCueText(strings.Join(r.lines, " "))
		if text == "" {
			continue
		}
		if strings.Contains(strings.Join(r.lines, " "), "&") && text != strings.Join(r.lines, " ") {
			*dirty = true
		}
		out = append(out, Cue{Start: r.start, End: r.end, Text: text})
	}
	if len(out) == 0 && hadKaraoke {
		// Nothing classified as committed; strip everything.
		for _, r := range raw {
			text := stripCueText(strings.Join(r.lines, " "))
			if text == "" {
				continue
			}
			out = append(out, Cue{Start: r.start, End: r.end, Text: text})
		}
	}
	return dedupConsecutive(out)
}

func dedupConsecutive(cues []Cue) []Cue {
	if len(cues) == 0 {
		return cues
	}
	out := make([]Cue, 0, len(cues))
	var last string
	for _, c := range cues {
		if c.Text == last {
			if n := len(out); n > 0 && c.End > out[n-1].End {
				out[n-1].End = c.End
			}
			continue
		}
		out = append(out, c)
		last = c.Text
	}
	return out
}
