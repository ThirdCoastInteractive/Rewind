package stitch

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf8"
)

// TeaserCaptionMargins describes normalized safe-area margins for a 9:16 canvas.
type TeaserCaptionMargins struct {
	Left   float64 `json:"left"`
	Right  float64 `json:"right"`
	Top    float64 `json:"top"`
	Bottom float64 `json:"bottom"`
}

// TeaserCaptionOptions controls deterministic teaser caption formatting.
type TeaserCaptionOptions struct {
	CaptionIDs      []string             `json:"caption_ids,omitempty"`
	Style           string               `json:"style,omitempty"`
	MaxCharsPerLine int                  `json:"max_chars_per_line,omitempty"`
	MaxLines        int                  `json:"max_lines,omitempty"`
	MaxPhraseChars  int                  `json:"max_phrase_chars,omitempty"`
	Margins         TeaserCaptionMargins `json:"margins,omitempty"`
	CanvasWidth     int                  `json:"canvas_width,omitempty"`
	CanvasHeight    int                  `json:"canvas_height,omitempty"`
}

// TeaserCaptionSource preserves the archive source mapping for a formatted cue.
type TeaserCaptionSource struct {
	CaptionID     string `json:"caption_id"`
	SourceVideoID string `json:"source_video_id,omitempty"`
	SourceStartUS int64  `json:"source_start_us"`
	SourceEndUS   int64  `json:"source_end_us"`
	URI           string `json:"uri,omitempty"`
	WebPath       string `json:"web_path,omitempty"`
}

// TeaserCaptionResult is the pure formatting result and its review warnings.
type TeaserCaptionResult struct {
	Captions     []Caption             `json:"captions"`
	Sources      []TeaserCaptionSource `json:"sources"`
	Warnings     []string              `json:"warnings,omitempty"`
	Alignment    map[string]string     `json:"alignment"`
	Margins      TeaserCaptionMargins  `json:"margins"`
	DeleteIDs    []string              `json:"delete_ids,omitempty"`
	Replacements map[string][]string   `json:"replacements,omitempty"`
}

// NewTeaserCaptionOperation creates a stable operation that can optionally import and style captions atomically.
func NewTeaserCaptionOperation(opts TeaserCaptionOptions, importSegmentID, importLanguage string) (Operation, error) {
	return Operation{Type: "style_teaser_captions", ImportSegmentID: importSegmentID, ImportLanguage: importLanguage, TeaserCaption: &opts}, nil
}

const (
	defaultTeaserChars = 32
	defaultTeaserLines = 2
	defaultPhraseChars = 64
)

// StyleTeaserCaptions formats selected captions for a 9:16 teaser without changing their evidence timing.
func StyleTeaserCaptions(d Document, opts TeaserCaptionOptions) (TeaserCaptionResult, error) {
	if err := Validate(d); err != nil {
		return TeaserCaptionResult{}, err
	}
	opts = teaserOptionsForDocument(d, opts)
	chars, lines, phraseChars, margins, style, err := validateTeaserCaptionOptions(opts)
	if err != nil {
		return TeaserCaptionResult{}, err
	}
	selected := map[string]bool{}
	for _, id := range opts.CaptionIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			return TeaserCaptionResult{}, fmt.Errorf("caption_ids cannot contain an empty id")
		}
		selected[id] = true
	}
	if len(selected) == 0 {
		for _, c := range d.Captions {
			selected[c.ID] = true
		}
	}

	out := TeaserCaptionResult{Margins: margins, Alignment: map[string]string{}, Replacements: map[string][]string{}}
	seen := map[string]bool{}
	for _, c := range d.Captions {
		if !selected[c.ID] {
			continue
		}
		seen[c.ID] = true
		formatted, warnings := formatTeaserCaption(c, style, chars, lines, phraseChars)
		if c.Alignment == "valid" && verifiedCaptionWords(c) && (len(formatted) > 1 || formatted[0].Text != c.Text) {
			out.DeleteIDs = append(out.DeleteIDs, c.ID)
			for i := range formatted {
				formatted[i].ID = teaserPhraseID(c.ID, i+1)
			}
			out.Replacements[c.ID] = make([]string, 0, len(formatted))
			for _, x := range formatted {
				out.Replacements[c.ID] = append(out.Replacements[c.ID], x.ID)
			}
		}
		out.Captions = append(out.Captions, formatted...)
		out.Warnings = append(out.Warnings, warnings...)
		for _, x := range formatted {
			state := x.Alignment
			if state == "" {
				state = "unaligned"
			}
			out.Alignment[x.ID] = state
			out.Sources = append(out.Sources, teaserCaptionSource(x))
		}
	}
	for id := range selected {
		if !seen[id] {
			return TeaserCaptionResult{}, fmt.Errorf("caption %q not found", id)
		}
	}
	if len(out.Captions) == 0 {
		return TeaserCaptionResult{}, fmt.Errorf("no captions selected")
	}
	return out, nil
}

// ValidateTeaserCaptionOptions validates formatting controls before captions are imported.
func ValidateTeaserCaptionOptions(opts TeaserCaptionOptions) error {
	_, _, _, _, _, err := validateTeaserCaptionOptions(opts)
	return err
}

// TeaserCaptionReport describes committed captions without changing their text or timing.
func TeaserCaptionReport(d Document, opts TeaserCaptionOptions, ids []string) (TeaserCaptionResult, error) {
	opts = teaserOptionsForDocument(d, opts)
	_, _, _, margins, style, err := validateTeaserCaptionOptions(opts)
	if err != nil {
		return TeaserCaptionResult{}, err
	}
	selected := map[string]bool{}
	for _, id := range ids {
		selected[strings.TrimSpace(id)] = true
	}
	if len(selected) == 0 {
		for _, c := range d.Captions {
			selected[c.ID] = true
		}
	}
	r := TeaserCaptionResult{Margins: margins, Alignment: map[string]string{}}
	for _, c := range d.Captions {
		if !selected[c.ID] {
			continue
		}
		r.Captions = append(r.Captions, c)
		r.Sources = append(r.Sources, teaserCaptionSource(c))
		state := c.Alignment
		if state == "" {
			state = "unaligned"
		}
		r.Alignment[c.ID] = state
		if state != "valid" {
			warning := fmt.Sprintf("caption %s needs alignment; phrase timing remains at source cue level", c.ID)
			if style.WordHighlight {
				warning = fmt.Sprintf("caption %s needs alignment before word highlighting", c.ID)
			}
			r.Warnings = append(r.Warnings, warning)
		}
	}
	if len(r.Captions) == 0 {
		return TeaserCaptionResult{}, fmt.Errorf("no committed captions selected")
	}
	return r, nil
}

func teaserOptionsForDocument(d Document, opts TeaserCaptionOptions) TeaserCaptionOptions {
	if opts.CanvasWidth == 0 && d.Width > 0 {
		opts.CanvasWidth = d.Width
	}
	if opts.CanvasHeight == 0 && d.Height > 0 {
		opts.CanvasHeight = d.Height
	}
	return opts
}

func validateTeaserCaptionOptions(opts TeaserCaptionOptions) (int, int, int, TeaserCaptionMargins, CaptionStyle, error) {
	chars := opts.MaxCharsPerLine
	if chars == 0 {
		chars = defaultTeaserChars
	}
	if chars < 1 || chars > 160 {
		return 0, 0, 0, TeaserCaptionMargins{}, CaptionStyle{}, fmt.Errorf("max_chars_per_line must be between 1 and 160")
	}
	lines := opts.MaxLines
	if lines == 0 {
		lines = defaultTeaserLines
	}
	if lines < 1 || lines > 6 {
		return 0, 0, 0, TeaserCaptionMargins{}, CaptionStyle{}, fmt.Errorf("max_lines must be between 1 and 6")
	}
	phraseChars := opts.MaxPhraseChars
	if phraseChars == 0 {
		phraseChars = chars * lines
		if phraseChars < defaultPhraseChars {
			phraseChars = defaultPhraseChars
		}
	}
	if phraseChars < chars || phraseChars > 480 {
		return 0, 0, 0, TeaserCaptionMargins{}, CaptionStyle{}, fmt.Errorf("max_phrase_chars must be between max_chars_per_line and 480")
	}
	margins, err := normalizeTeaserMargins(opts.Margins)
	if err != nil {
		return 0, 0, 0, TeaserCaptionMargins{}, CaptionStyle{}, err
	}
	width, height := opts.CanvasWidth, opts.CanvasHeight
	if width == 0 {
		width = 1080
	}
	if height == 0 {
		height = 1920
	}
	if width < 90 || height < 160 || float64(height)/float64(width) < 1.6 || float64(height)/float64(width) > 1.9 {
		return 0, 0, 0, TeaserCaptionMargins{}, CaptionStyle{}, fmt.Errorf("canvas must be a bounded 9:16 portrait canvas")
	}
	style, err := teaserStyle(opts.Style, width, height, margins)
	if err != nil {
		return 0, 0, 0, TeaserCaptionMargins{}, CaptionStyle{}, err
	}
	return chars, lines, phraseChars, margins, style, nil
}

func normalizeTeaserMargins(m TeaserCaptionMargins) (TeaserCaptionMargins, error) {
	if m.Left == 0 && m.Right == 0 && m.Top == 0 && m.Bottom == 0 {
		m = TeaserCaptionMargins{Left: .08, Right: .08, Top: .08, Bottom: .10}
	}
	for _, v := range []float64{m.Left, m.Right, m.Top, m.Bottom} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v >= .5 {
			return TeaserCaptionMargins{}, fmt.Errorf("safe margins must be between 0 and 0.5")
		}
	}
	if m.Left+m.Right >= 1 || m.Top+m.Bottom >= 1 {
		return TeaserCaptionMargins{}, fmt.Errorf("safe margins leave no usable 9:16 canvas")
	}
	return m, nil
}

func teaserStyle(name string, width, height int, margins TeaserCaptionMargins) (CaptionStyle, error) {
	scale := math.Min(float64(width)/1080, float64(height)/1920)
	fontSize := 64 * scale
	if fontSize < 24 {
		fontSize = 24
	}
	if fontSize > 96 {
		fontSize = 96
	}
	safeWidth := 1 - margins.Left - margins.Right
	x := float64(width) * (margins.Left + safeWidth/2)
	y := float64(height) * (1 - margins.Bottom)
	if minY := float64(height)*margins.Top + fontSize; y < minY {
		return CaptionStyle{}, fmt.Errorf("safe margins leave no readable caption block")
	}
	style := CaptionStyle{Font: "Tomorrow", Color: "#ffffff", FontSize: fontSize, X: x, Y: y, SafeArea: true}
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "basic", "clean":
	case "bold":
		style.Bold = true
	case "outlined", "outline":
		style.OutlineColor = "#000000"
		style.OutlineWidth = 2
	case "karaoke", "highlight":
		style.Bold = true
		style.WordHighlight = true
		style.HighlightColor = "#ffd166"
	default:
		return CaptionStyle{}, fmt.Errorf("unsupported teaser caption style %q", name)
	}
	return style, nil
}

func formatTeaserCaption(c Caption, style CaptionStyle, maxChars, maxLines, maxPhraseChars int) ([]Caption, []string) {
	warnings := []string{}
	if c.Alignment == "valid" && verifiedCaptionWords(c) {
		// ASS word highlighting reconstructs one dialogue for each active word.
		// Keep those phrases to one physical line so every highlighted dialogue
		// has the same readable layout.
		groupLines := maxLines
		if style.WordHighlight {
			groupLines = 1
		}
		groups := groupTeaserWords(c.Words, maxChars, groupLines, maxPhraseChars)
		if len(groups) > 0 {
			out := make([]Caption, 0, len(groups))
			for i, words := range groups {
				x := c
				x.ID = teaserPhraseID(c.ID, i)
				x.Text = teaserWordsText(words, maxChars)
				x.Words = append([]Word(nil), words...)
				x.StartUS = words[0].StartUS
				x.EndUS = words[len(words)-1].EndUS
				x.SourceStartUS, x.SourceEndUS = sourceBoundsForWords(c, words[0], words[len(words)-1])
				x.Style = style
				x.Alignment = "valid"
				// The original alignment key hashes the old cue. Preserve words and source
				// state, but do not claim the old key describes this newly split phrase.
				x.AlignmentKey = ""
				out = append(out, x)
			}
			if len(out) > 1 || out[0].Text != c.Text {
				warnings = append(warnings, fmt.Sprintf("caption %s was split using verified word timings", c.ID))
			}
			return out, warnings
		}
	}

	if style.WordHighlight {
		style.WordHighlight = false
		warnings = append(warnings, fmt.Sprintf("caption %s needs alignment before word highlighting; phrase timing retained", c.ID))
	}
	lines := wrapTeaserText(c.Text, maxChars)
	if len(lines) > maxLines {
		warnings = append(warnings, fmt.Sprintf("caption %s exceeds %d readable lines; alignment is needed for phrase timing", c.ID, maxLines))
	}
	x := c
	x.Text = strings.Join(lines, "\n")
	x.Style = style
	if c.Alignment != "valid" || !verifiedCaptionWords(c) {
		x.Alignment = "unaligned"
		x.Words = nil
		x.AlignmentKey = ""
		if c.Text != "" {
			warnings = append(warnings, fmt.Sprintf("caption %s has no verified word timings; source cue timing and provenance were retained", c.ID))
		}
	}
	return []Caption{x}, warnings
}

func verifiedCaptionWords(c Caption) bool {
	if c.Alignment != "valid" || len(c.Words) == 0 {
		return false
	}
	parts := make([]string, 0, len(c.Words))
	last := c.StartUS
	for _, w := range c.Words {
		if strings.TrimSpace(w.Text) == "" || w.StartUS < c.StartUS || w.EndUS <= w.StartUS || w.EndUS > c.EndUS || w.StartUS < last {
			return false
		}
		parts = append(parts, w.Text)
		last = w.EndUS
	}
	return normalizeTeaserText(strings.Join(parts, " ")) == normalizeTeaserText(c.Text)
}

// normalizeTeaserText is deliberately small and deterministic: it collapses
// whitespace and applies case folding while retaining punctuation and
// non-Latin letters, so timing cannot be attached to text that differs in
// substance from the source cue.
func normalizeTeaserText(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(s)), " "))
}

func groupTeaserWords(words []Word, maxChars, maxLines, maxPhraseChars int) [][]Word {
	var groups [][]Word
	var group []Word
	lineChars := 0
	lineCount := 1
	phraseChars := 0
	flush := func() {
		if len(group) > 0 {
			groups = append(groups, group)
		}
		group = nil
		lineChars, lineCount, phraseChars = 0, 1, 0
	}
	for _, w := range words {
		wordChars := utf8.RuneCountInString(strings.TrimSpace(w.Text))
		if wordChars == 0 {
			continue
		}
		space := 0
		if lineChars > 0 {
			space = 1
		}
		nextLineChars := lineChars + space + wordChars
		nextLineCount := lineCount
		if lineChars > 0 && nextLineChars > maxChars {
			nextLineCount++
			nextLineChars = wordChars
		}
		nextPhraseChars := phraseChars + space + wordChars
		if len(group) > 0 && (nextLineCount > maxLines || nextPhraseChars > maxPhraseChars) {
			flush()
			space = 0
			nextLineChars, nextLineCount, nextPhraseChars = wordChars, 1, wordChars
		}
		group = append(group, w)
		lineChars, lineCount, phraseChars = nextLineChars, nextLineCount, nextPhraseChars
	}
	flush()
	return groups
}

func teaserWordsText(words []Word, maxChars int) string {
	parts := make([]string, 0, len(words))
	for _, w := range words {
		parts = append(parts, strings.TrimSpace(w.Text))
	}
	return strings.Join(wrapTeaserText(strings.Join(parts, " "), maxChars), "\n")
}

// TeaserCaptionOperations turns a deterministic formatting result into typed Stitch operations.
func TeaserCaptionOperations(formatted TeaserCaptionResult) []Operation {
	return teaserCaptionOperations(formatted, nil)
}

// TeaserCaptionOperationsForDocument preserves timing-link memberships while replacing split captions.
func TeaserCaptionOperationsForDocument(before Document, formatted TeaserCaptionResult) []Operation {
	return teaserCaptionOperations(formatted, before.TimingLinks)
}

func teaserCaptionOperations(formatted TeaserCaptionResult, groups []Group) []Operation {
	ops := make([]Operation, 0, len(formatted.DeleteIDs)+len(formatted.Captions))
	seen := map[string]bool{}
	for _, id := range formatted.DeleteIDs {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ops = append(ops, Operation{Type: "delete_caption", TargetID: id})
	}
	for i := range formatted.Captions {
		c := formatted.Captions[i]
		ops = append(ops, Operation{Type: "upsert_caption", Caption: &c})
	}
	oldIDs := make([]string, 0, len(formatted.Replacements))
	for old := range formatted.Replacements {
		oldIDs = append(oldIDs, old)
	}
	sort.Strings(oldIDs)
	for _, old := range oldIDs {
		replacements := formatted.Replacements[old]
		for _, g := range groups {
			if !contains(g.Members, old) {
				continue
			}
			members := make([]string, 0, len(g.Members)+len(replacements)-1)
			for _, id := range g.Members {
				if id == old {
					members = append(members, replacements...)
				} else {
					members = append(members, id)
				}
			}
			if len(members) >= 2 {
				ops = append(ops, Operation{Type: "link_timing", Group: &Group{ID: g.ID, Members: members}})
			}
		}
	}
	return ops
}

func wrapTeaserText(text string, maxChars int) []string {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return []string{""}
	}
	var lines []string
	current := ""
	flush := func() {
		if current != "" {
			lines = append(lines, current)
			current = ""
		}
	}
	for _, field := range fields {
		if utf8.RuneCountInString(field) > maxChars {
			flush()
			runes := []rune(field)
			for len(runes) > maxChars {
				lines = append(lines, string(runes[:maxChars]))
				runes = runes[maxChars:]
			}
			current = string(runes)
			continue
		}
		candidate := field
		if current != "" {
			candidate = current + " " + field
		}
		if utf8.RuneCountInString(candidate) > maxChars {
			flush()
		}
		if current == "" {
			current = field
		} else if utf8.RuneCountInString(current+" "+field) <= maxChars {
			current += " " + field
		}
	}
	flush()
	return lines
}

func teaserPhraseID(original string, n int) string {
	if n == 0 {
		return original
	}
	h := sha256.Sum256([]byte(fmt.Sprintf("teaser-caption:%s:%d", original, n)))
	return "teaser-" + hex.EncodeToString(h[:16])
}

func sourceBoundsForWords(c Caption, first, last Word) (int64, int64) {
	if c.SourceEndUS <= c.SourceStartUS {
		return c.SourceStartUS, c.SourceEndUS
	}
	start := c.SourceStartUS + first.StartUS - c.StartUS
	end := c.SourceStartUS + last.EndUS - c.StartUS
	if start < c.SourceStartUS {
		start = c.SourceStartUS
	}
	if end > c.SourceEndUS {
		end = c.SourceEndUS
	}
	return start, end
}

func teaserCaptionSource(c Caption) TeaserCaptionSource {
	s := TeaserCaptionSource{CaptionID: c.ID, SourceVideoID: c.SourceVideoID, SourceStartUS: c.SourceStartUS, SourceEndUS: c.SourceEndUS}
	if c.SourceVideoID != "" {
		s.URI = "rewind://video/" + c.SourceVideoID
		s.WebPath = fmt.Sprintf("/videos/%s?t=%.3f", c.SourceVideoID, float64(c.SourceStartUS)/1e6)
	}
	return s
}
