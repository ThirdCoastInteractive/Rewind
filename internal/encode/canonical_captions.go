package encode

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"thirdcoast.systems/rewind/internal/stitch"
)

// CompileCanonicalCaptions produces a deterministic caption sidecar for an
// immutable document. Times remain on the project timeline; the renderer can
// offset them when rendering a range.
func CompileCanonicalCaptions(d stitch.Document, format string, wordHighlight bool) (string, error) {
	if format != "srt" && format != "vtt" && format != "ass" {
		return "", fmt.Errorf("unsupported caption format %q", format)
	}
	if format == "ass" {
		for _, c := range d.Captions {
			if c.Style.WordHighlight && (c.Alignment != "valid" || len(c.Words) == 0) {
				return "", fmt.Errorf("caption %q has pending word alignment", c.ID)
			}
		}
	}
	var b strings.Builder
	if format == "vtt" {
		b.WriteString("WEBVTT\n\n")
	} else if format == "ass" {
		writeASSHeader(&b, d)
	}
	for i, c := range d.Captions {
		if c.EndUS <= c.StartUS || c.Text == "" {
			continue
		}
		switch format {
		case "srt":
			fmt.Fprintf(&b, "%d\n%s --> %s\n%s\n\n", i+1, srtTime(c.StartUS), srtTime(c.EndUS), strings.ReplaceAll(c.Text, "\r\n", "\n"))
		case "vtt":
			fmt.Fprintf(&b, "%s --> %s\n%s\n\n", vttTime(c.StartUS), vttTime(c.EndUS), strings.ReplaceAll(c.Text, "\r\n", "\n"))
		case "ass":
			if c.Style.WordHighlight {
				c.Style.Font = resolvedCaptionFont(c, d)
				if err := writeWordASS(&b, c, d.Width, d.Height); err != nil {
					return "", err
				}
			} else {
				style := c.Style
				style.Font = resolvedCaptionFont(c, d)
				fmt.Fprintf(&b, "Dialogue: 0,%s,%s,%s,,0,0,0,,%s%s\n", assTimeUS(c.StartUS), assTimeUS(c.EndUS), assCaptionStyleName(style), assStyle(style, d.Width, d.Height), assEscape(c.Text))
			}
		}
	}
	return b.String(), nil
}

func srtTime(us int64) string {
	return fmt.Sprintf("%02d:%02d:%02d,%03d", us/3_600_000_000, (us/60_000_000)%60, (us/1_000_000)%60, (us/1000)%1000)
}
func vttTime(us int64) string { return strings.Replace(srtTime(us), ",", ".", 1) }
func assTimeUS(us int64) string {
	return fmt.Sprintf("%d:%02d:%02d.%02d", us/3_600_000_000, (us/60_000_000)%60, (us/1_000_000)%60, (us/10_000)%100)
}

func writeASSHeader(b *strings.Builder, d stitch.Document) {
	font := strings.TrimSpace(d.Settings.CaptionFont)
	if font == "" {
		font = "Tomorrow"
	}
	fmt.Fprintf(b, "[Script Info]\nScriptType: v4.00+\nPlayResX: %d\nPlayResY: %d\n\n", d.Width, d.Height)
	b.WriteString("[V4+ Styles]\nFormat: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n")
	fmt.Fprintf(b, "Style: Default,%s,48,&H00FFFFFF,&H00FFFFFF,&H00000000,&H80000000,0,0,0,0,100,100,0,0,1,0,0,2,24,24,24,1\nStyle: Box,%s,48,&H00FFFFFF,&H00FFFFFF,&H00000000,&H80000000,0,0,0,0,100,100,0,0,3,0,0,2,24,24,24,1\n\n[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n", font, font)
}

func resolvedCaptionFont(c stitch.Caption, d stitch.Document) string {
	if family := strings.TrimSpace(c.Style.Font); family != "" {
		return family
	}
	if family := strings.TrimSpace(d.Settings.CaptionFont); family != "" {
		return family
	}
	return "Tomorrow"
}

func assStyle(s stitch.CaptionStyle, width, height int) string {
	var tags []string
	if s.Font != "" {
		tags = append(tags, "\\fn"+assEscapeTag(s.Font))
	}
	if s.FontSize > 0 {
		tags = append(tags, "\\fs"+strconv.Itoa(int(s.FontSize)))
	}
	if s.Color != "" {
		tags = append(tags, "\\c"+assColor(s.Color))
	}
	if s.OutlineColor != "" {
		tags = append(tags, "\\3c"+assColor(s.OutlineColor))
	}
	if s.OutlineWidth > 0 {
		tags = append(tags, "\\bord"+strconv.FormatFloat(s.OutlineWidth, 'f', -1, 64))
	}
	if s.Bold {
		tags = append(tags, "\\b1")
	}
	if s.Italic {
		tags = append(tags, "\\i1")
	}
	if s.Background != "" {
		tags = append(tags, "\\4c"+assColor(s.Background), "\\4a&H00&", "\\3c"+assColor(s.Background), "\\bord1")
	}
	x, y := s.X, s.Y
	if x == 0 && y == 0 {
		x, y = float64(width)/2, float64(height)-48
	}
	if s.SafeArea {
		x = clamp(x, float64(width)*.1, float64(width)*.9)
		y = clamp(y, float64(height)*.1, float64(height)*.9)
	}
	tags = append(tags, "\\an2", fmt.Sprintf("\\pos(%.0f,%.0f)", x, y))
	if len(tags) == 0 {
		return ""
	}
	return "{" + strings.Join(tags, "") + "}"
}

func assCaptionStyleName(s stitch.CaptionStyle) string {
	if s.Background != "" {
		return "Box"
	}
	return "Default"
}

func writeWordASS(b *strings.Builder, c stitch.Caption, width, height int) error {
	if c.Alignment != "valid" || len(c.Words) == 0 {
		return fmt.Errorf("caption %q has pending word alignment", c.ID)
	}
	for _, w := range c.Words {
		if w.StartUS < c.StartUS || w.EndUS <= w.StartUS || w.EndUS > c.EndUS || w.Text == "" {
			return fmt.Errorf("caption %q has invalid word timing", c.ID)
		}
		if !strings.Contains(c.Text, w.Text) {
			return fmt.Errorf("caption %q word %q cannot be aligned", c.ID, w.Text)
		}
	}
	// Emit non-overlapping intervals so the full caption remains visible during
	// pauses while only the active word receives the highlight colour.
	for i, w := range c.Words {
		start := c.StartUS
		if i > 0 {
			start = c.Words[i-1].EndUS
		}
		if start < w.StartUS {
			writeWordDialogue(b, c, start, w.StartUS, -1, width, height)
		}
		writeWordDialogue(b, c, w.StartUS, w.EndUS, i, width, height)
	}
	if end := c.Words[len(c.Words)-1].EndUS; end < c.EndUS {
		writeWordDialogue(b, c, end, c.EndUS, -1, width, height)
	}
	return nil
}

func writeWordDialogue(b *strings.Builder, c stitch.Caption, start, end int64, active, width, height int) {
	if end <= start {
		return
	}
	fmt.Fprintf(b, "Dialogue: 0,%s,%s,%s,,0,0,0,,%s%s\n", assTimeUS(start), assTimeUS(end), assCaptionStyleName(c.Style), assStyle(c.Style, width, height), wordHighlightedText(c.Text, c.Words, active, c.Style.HighlightColor))
}

func wordHighlightedText(full string, words []stitch.Word, active int, highlight string) string {
	var out strings.Builder
	pos := 0
	for i, w := range words {
		idx := strings.Index(full[pos:], w.Text)
		if idx < 0 {
			return ""
		}
		idx += pos
		out.WriteString(assEscape(full[pos:idx]))
		if i == active {
			if highlight == "" {
				highlight = "#ffff00"
			}
			out.WriteString("{\\c" + assColor(highlight) + "}")
		}
		out.WriteString(assEscape(full[idx : idx+len(w.Text)]))
		if i == active {
			out.WriteString("{\\c}")
		}
		pos = idx + len(w.Text)
	}
	out.WriteString(assEscape(full[pos:]))
	return out.String()
}

func assEscape(s string) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "�")
	}
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "{", `\{`)
	s = strings.ReplaceAll(s, "}", `\}`)
	s = strings.ReplaceAll(s, "\r\n", `\N`)
	s = strings.ReplaceAll(s, "\n", `\N`)
	return s
}
func assEscapeTag(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "{", ""), "}", "")
}
func assColor(s string) string {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) == 6 {
		return "&H00" + s[4:6] + s[2:4] + s[0:2]
	}
	return s
}
