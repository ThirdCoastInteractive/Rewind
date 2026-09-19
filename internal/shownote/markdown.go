// Package shownote contains the canonical Markdown document model used by the
// collaborative show workspace, the producer rundown, and MCP clients.
package shownote

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// ItemKind identifies a semantic rundown item parsed from Markdown.
type ItemKind string

const (
	// ItemHeading is a Markdown heading that groups following rundown items.
	ItemHeading ItemKind = "heading"
	// ItemReference is a playable or potentially playable media reference.
	ItemReference ItemKind = "reference"
	// ItemBreak is a non-playable production break cue.
	ItemBreak ItemKind = "break"
)

// ReferenceKind identifies the kind of media URI in a rundown reference.
type ReferenceKind string

const (
	// ReferenceVideo points at a Rewind video.
	ReferenceVideo ReferenceKind = "video"
	// ReferenceClip points at a saved Rewind clip.
	ReferenceClip ReferenceKind = "clip"
	// ReferenceMarker points at a saved Rewind marker.
	ReferenceMarker ReferenceKind = "marker"
	// ReferenceExternal points at an HTTP(S) source not yet materialized.
	ReferenceExternal ReferenceKind = "external"
	// ReferenceUnknown is syntactically link-like but not a supported source.
	ReferenceUnknown ReferenceKind = "unknown"
)

// Bounds describes an optional point or range attached to a reference.
type Bounds struct {
	Start   float64 `json:"start"`
	End     float64 `json:"end,omitempty"`
	HasTime bool    `json:"has_time"`
	IsRange bool    `json:"is_range"`
}

// Reference is the semantic projection of one media link occurrence.
type Reference struct {
	OccurrenceKey string        `json:"occurrence_key"`
	Ordinal       int           `json:"ordinal"`
	Kind          ReferenceKind `json:"kind"`
	URI           string        `json:"uri"`
	ObjectID      string        `json:"object_id,omitempty"`
	Label         string        `json:"label"`
	Context       string        `json:"context,omitempty"`
	SectionPath   []string      `json:"section_path,omitempty"`
	Bounds        Bounds        `json:"bounds"`
	LineStart     int           `json:"line_start"`
	LineEnd       int           `json:"line_end"`
	Bare          bool          `json:"bare"`
	Diagnostic    string        `json:"diagnostic,omitempty"`
}

// Item is a heading, reference, or break in document order.
type Item struct {
	Kind        ItemKind   `json:"kind"`
	Line        int        `json:"line"`
	Level       int        `json:"level,omitempty"`
	Text        string     `json:"text,omitempty"`
	SectionPath []string   `json:"section_path,omitempty"`
	Reference   *Reference `json:"reference,omitempty"`
}

// Document is the rebuildable semantic projection of Markdown.
type Document struct {
	Items      []Item      `json:"items"`
	References []Reference `json:"references"`
}

var (
	headingPattern = regexp.MustCompile(`^(#{1,6})[\t ]+(.+?)[\t ]*$`)
	breakPattern   = regexp.MustCompile(`(?i)^\s*>\s*break(?:\s*[—-]\s*(.*))?\s*$`)
	linkPattern    = regexp.MustCompile(`^\s*(?:(?:\d+[.)]|[-+*])\s+)?\[([^\]]*)\]\(([^)\s]+)\)(.*)$`)
	barePattern    = regexp.MustCompile(`^\s*(?:(?:\d+[.)]|[-+*])\s+)?(https?://\S+)(.*)$`)
	rewindPattern  = regexp.MustCompile(`^rewind://(video|clip|marker)/([0-9A-Za-z_-]+)$`)
	relativeVideo  = regexp.MustCompile(`^/(?:videos?|watch)/([0-9A-Za-z_-]+)(?:/.*)?$`)
	relativeClip   = regexp.MustCompile(`^/(?:clips?|videos/[0-9A-Za-z_-]+/clips?)/([0-9A-Za-z_-]+)(?:/.*)?$`)
	relativeMarker = regexp.MustCompile(`^/(?:markers?|videos/[0-9A-Za-z_-]+/markers?)/([0-9A-Za-z_-]+)(?:/.*)?$`)
	timePattern    = regexp.MustCompile(`(?:^|\s)@\s*([0-9]+:[0-9]{2}(?::[0-9]{2})?)(?:\s*(?:–|—|-|\.\.)\s*([0-9]+:[0-9]{2}(?::[0-9]{2})?))?`)
	atPattern      = regexp.MustCompile(`(?:^|\s)@\s*([^\s]+(?:\s*(?:–|—|-|\.\.)\s*[^\s]+)?)`)
)

// ParseMarkdown builds a semantic rundown projection without changing or
// rejecting the surrounding Markdown. Invalid references carry diagnostics.
func ParseMarkdown(markdown string) Document {
	lines := strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n")
	doc := Document{}
	headings := make([]string, 6)
	uriOccurrences := make(map[string]int)
	var active *Reference

	flushActive := func() {
		if active == nil {
			return
		}
		active.Context = strings.TrimSpace(active.Context)
		occurrence := uriOccurrences[active.URI]
		active.OccurrenceKey = occurrenceKey(active.URI, occurrence)
		uriOccurrences[active.URI] = occurrence + 1
		doc.References = append(doc.References, *active)
		ref := doc.References[len(doc.References)-1]
		doc.Items = append(doc.Items, Item{
			Kind: ItemReference, Line: ref.LineStart, SectionPath: ref.SectionPath, Reference: &ref,
		})
		active = nil
	}

	for idx, line := range lines {
		lineNo := idx + 1
		if active != nil && isIndentedContext(line) {
			text := strings.TrimSpace(line)
			if text != "" {
				if active.Context != "" {
					active.Context += "\n"
				}
				active.Context += text
				active.LineEnd = lineNo
			}
			continue
		}
		flushActive()

		if match := headingPattern.FindStringSubmatch(line); match != nil {
			level := len(match[1])
			text := strings.TrimSpace(match[2])
			headings[level-1] = text
			for i := level; i < len(headings); i++ {
				headings[i] = ""
			}
			doc.Items = append(doc.Items, Item{Kind: ItemHeading, Line: lineNo, Level: level, Text: text, SectionPath: currentSection(headings)})
			continue
		}
		if match := breakPattern.FindStringSubmatch(line); match != nil {
			doc.Items = append(doc.Items, Item{Kind: ItemBreak, Line: lineNo, Text: strings.TrimSpace(match[1]), SectionPath: currentSection(headings)})
			continue
		}

		label, rawURI, suffix, bare, ok := referencePrefix(line)
		if !ok {
			continue
		}
		kind, objectID, diagnostic := classifyURI(rawURI)
		bounds, boundsDiagnostic := parseBounds(suffix)
		if diagnostic == "" {
			diagnostic = boundsDiagnostic
		}
		active = &Reference{
			Ordinal: len(doc.References), Kind: kind, URI: rawURI, ObjectID: objectID,
			Label: label, SectionPath: currentSection(headings), Bounds: bounds,
			LineStart: lineNo, LineEnd: lineNo, Bare: bare, Diagnostic: diagnostic,
		}
	}
	flushActive()
	return doc
}

// AddedExternalReferences returns external media occurrences added between
// two snapshots. Existing occurrences are consumed in document order so a
// newly accepted duplicate is still returned exactly once.
func AddedExternalReferences(before, after string) []Reference {
	existing := make(map[string]int)
	for _, ref := range ParseMarkdown(before).References {
		if ref.Kind == ReferenceExternal {
			existing[ref.URI]++
		}
	}

	added := make([]Reference, 0)
	for _, ref := range ParseMarkdown(after).References {
		if ref.Kind != ReferenceExternal {
			continue
		}
		if existing[ref.URI] > 0 {
			existing[ref.URI]--
			continue
		}
		added = append(added, ref)
	}
	return added
}

func referencePrefix(line string) (label, rawURI, suffix string, bare, ok bool) {
	if match := linkPattern.FindStringSubmatch(line); match != nil {
		return strings.TrimSpace(match[1]), strings.TrimSpace(match[2]), match[3], false, true
	}
	if match := barePattern.FindStringSubmatch(line); match != nil {
		raw := strings.TrimRight(match[1], ".,;)")
		return raw, raw, strings.TrimPrefix(match[1], raw) + match[2], true, true
	}
	return "", "", "", false, false
}

func classifyURI(raw string) (ReferenceKind, string, string) {
	if match := rewindPattern.FindStringSubmatch(raw); match != nil {
		return ReferenceKind(match[1]), match[2], ""
	}
	for _, candidate := range []struct {
		pattern *regexp.Regexp
		kind    ReferenceKind
	}{{relativeClip, ReferenceClip}, {relativeMarker, ReferenceMarker}, {relativeVideo, ReferenceVideo}} {
		if match := candidate.pattern.FindStringSubmatch(raw); match != nil {
			return candidate.kind, match[1], ""
		}
	}
	parsed, err := url.Parse(raw)
	if err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" {
		return ReferenceExternal, "", ""
	}
	return ReferenceUnknown, "", "unsupported media reference"
}

func parseBounds(suffix string) (Bounds, string) {
	match := timePattern.FindStringSubmatch(suffix)
	if match == nil {
		if atPattern.MatchString(suffix) {
			return Bounds{}, "invalid timestamp; use M:SS or H:MM:SS"
		}
		return Bounds{}, ""
	}
	start, err := parseTimestamp(match[1])
	if err != nil {
		return Bounds{}, err.Error()
	}
	bounds := Bounds{Start: start, HasTime: true}
	if match[2] == "" {
		return bounds, ""
	}
	end, err := parseTimestamp(match[2])
	if err != nil {
		return Bounds{}, err.Error()
	}
	if end <= start {
		return Bounds{}, "range end must be after its start"
	}
	bounds.End = end
	bounds.IsRange = true
	return bounds, ""
}

func parseTimestamp(value string) (float64, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 && len(parts) != 3 {
		return 0, fmt.Errorf("invalid timestamp %q", value)
	}
	values := make([]int, len(parts))
	for i, part := range parts {
		v, err := strconv.Atoi(part)
		if err != nil || v < 0 {
			return 0, fmt.Errorf("invalid timestamp %q", value)
		}
		values[i] = v
	}
	if values[len(values)-1] >= 60 || (len(values) == 3 && values[1] >= 60) {
		return 0, fmt.Errorf("invalid timestamp %q", value)
	}
	if len(values) == 2 {
		return float64(values[0]*60 + values[1]), nil
	}
	return float64(values[0]*3600 + values[1]*60 + values[2]), nil
}

func isIndentedContext(line string) bool {
	if strings.TrimSpace(line) == "" {
		return false
	}
	return strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "\t")
}

func currentSection(headings []string) []string {
	path := make([]string, 0, len(headings))
	for _, heading := range headings {
		if heading != "" {
			path = append(path, heading)
		}
	}
	return path
}

func occurrenceKey(uri string, occurrence int) string {
	identity := fmt.Sprintf("%s\x00%d", uri, occurrence)
	digest := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(digest[:12])
}

// FormatTimestamp returns the canonical readable timestamp form.
func FormatTimestamp(seconds float64) string {
	total := int(seconds)
	hours := total / 3600
	minutes := (total % 3600) / 60
	secs := total % 60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, secs)
	}
	return fmt.Sprintf("%d:%02d", minutes, secs)
}

// RewriteReferenceURI replaces one projected reference URI on its expected
// source line, refusing ambiguous or stale matches.
func RewriteReferenceURI(markdown string, line int, oldURI, newURI string) (string, error) {
	lines := strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n")
	if line < 1 || line > len(lines) {
		return "", fmt.Errorf("reference line is outside the current document")
	}
	if strings.Count(lines[line-1], oldURI) != 1 {
		return "", fmt.Errorf("reference moved or changed")
	}
	lines[line-1] = strings.Replace(lines[line-1], oldURI, newURI, 1)
	return strings.Join(lines, "\n"), nil
}

// RewriteReferenceBounds refreshes the readable timestamp attached to one
// reference without changing the saved library object.
func RewriteReferenceBounds(markdown string, line int, uri string, bounds Bounds) (string, error) {
	if !bounds.HasTime {
		return "", fmt.Errorf("reference has no saved timestamp")
	}
	lines := strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n")
	if line < 1 || line > len(lines) {
		return "", fmt.Errorf("reference line is outside the current document")
	}
	uriOffset := strings.Index(lines[line-1], uri)
	if uriOffset < 0 || strings.Count(lines[line-1], uri) != 1 {
		return "", fmt.Errorf("reference moved or changed")
	}
	suffixOffset := uriOffset + len(uri)
	suffix := lines[line-1][suffixOffset:]
	formatted := " @ " + FormatTimestamp(bounds.Start)
	if bounds.IsRange {
		formatted += "–" + FormatTimestamp(bounds.End)
	}
	if match := atPattern.FindStringIndex(suffix); match != nil {
		suffix = suffix[:match[0]] + formatted + suffix[match[1]:]
	} else {
		suffix += formatted
	}
	lines[line-1] = lines[line-1][:suffixOffset] + suffix
	return strings.Join(lines, "\n"), nil
}
