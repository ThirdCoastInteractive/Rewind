package ffmpeg

import (
	"crypto/sha256"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"thirdcoast.systems/rewind/pkg/captions"
	"thirdcoast.systems/rewind/pkg/typefaces"
)

// BurnCaptionsEnabled reports the optional stitch "burn captions" flag.
// Missing or empty globals leave it off.
func BurnCaptionsEnabled(specs []FilterSpec) bool {
	for _, s := range specs {
		if s.Type != "burn_captions" {
			continue
		}
		if s.Params == nil {
			return true
		}
		switch v := s.Params["enabled"].(type) {
		case bool:
			return v
		case string:
			return v != "false" && v != "0" && v != ""
		case float64:
			return v != 0
		default:
			return true
		}
	}
	return false
}

// BurnCaptionsFont is the optional family on the burn_captions control filter.
func BurnCaptionsFont(specs []FilterSpec) string {
	for _, s := range specs {
		if s.Type != "burn_captions" || s.Params == nil {
			continue
		}
		if v, ok := s.Params["font"].(string); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// BurnCaptionsFilter writes a styled ASS sidecar for the clip range and
// returns an ass= filter. Empty string means no captions (missing or empty).
func BurnCaptionsFilter(videoDir, videoID string, start, dur float64, family string) (string, error) {
	vtt := findCaptionVTT(videoDir, videoID)
	if vtt == "" {
		return "", nil
	}
	doc, err := captions.CleanFile(vtt)
	if err != nil {
		return "", err
	}
	cues := sliceCuesForBurn(doc.Cues, start, start+dur)
	if len(cues) == 0 {
		return "", nil
	}
	fontName, fontDir, err := captionFont(family)
	if err != nil {
		return "", err
	}
	assPath, err := writeBurnASS(cues, fontName)
	if err != nil {
		return "", err
	}
	return "ass='" + escapeDrawtextPath(assPath) + "':fontsdir='" + escapeDrawtextPath(fontDir) + "'", nil
}

func findCaptionVTT(dir, videoID string) string {
	if dir == "" || videoID == "" {
		return ""
	}
	for _, name := range []string{
		videoID + ".captions.en.vtt",
		videoID + ".captions.und.vtt",
	} {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && st.Size() > 0 {
			return p
		}
	}
	matches, _ := filepath.Glob(filepath.Join(dir, videoID+".captions.*.vtt"))
	for _, p := range matches {
		if strings.HasSuffix(strings.ToLower(p), ".src.vtt") {
			continue
		}
		if st, err := os.Stat(p); err == nil && st.Size() > 0 {
			return p
		}
	}
	return ""
}

func sliceCuesForBurn(cues []captions.Cue, start, end float64) []captions.Cue {
	var out []captions.Cue
	for _, c := range cues {
		if c.End <= start || c.Start >= end {
			continue
		}
		if !spokenCaption(c.Text) {
			continue
		}
		ns := c.Start - start
		ne := c.End - start
		if ns < 0 {
			ns = 0
		}
		if ne > end-start {
			ne = end - start
		}
		if ne-ns < 0.12 {
			continue
		}
		out = append(out, captions.Cue{Start: ns, End: ne, Text: strings.TrimSpace(c.Text)})
	}
	return out
}

func spokenCaption(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	if strings.HasPrefix(t, "♪") || strings.HasPrefix(t, "♫") {
		return false
	}
	if strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
		return false
	}
	if strings.HasPrefix(t, "(") && strings.HasSuffix(t, ")") {
		return false
	}
	return true
}

func captionFont(family string) (name, dir string, err error) {
	_, regular, err := TitleFontPaths()
	bundledDir := ""
	if err == nil && regular != "" {
		bundledDir = filepath.Dir(regular)
	}
	if face, ok := typefaces.Lookup(family); ok {
		if face.Dir != "" {
			return face.Family, face.Dir, nil
		}
		if bundledDir != "" {
			return face.Family, bundledDir, nil
		}
	}
	if err != nil || regular == "" {
		return "", "", err
	}
	return "Tomorrow", bundledDir, nil
}

func writeBurnASS(cues []captions.Cue, fontName string) (string, error) {
	if fontName == "" {
		fontName = "Tomorrow"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", fontName)
	for _, c := range cues {
		fmt.Fprintf(&b, "%.3f\t%.3f\t%s\n", c.Start, c.End, c.Text)
	}
	sum := sha256.Sum256([]byte(b.String()))
	dir := filepath.Join(os.TempDir(), "rewind-subs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, fmt.Sprintf("%x.ass", sum[:10]))
	if st, err := os.Stat(dest); err == nil && st.Size() > 0 {
		return dest, nil
	}
	body := burnASSDocument(cues, fontName)
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return dest, nil
}

func burnASSDocument(cues []captions.Cue, fontName string) string {
	if fontName == "" {
		fontName = "Tomorrow"
	}
	var b strings.Builder
	b.WriteString("[Script Info]\n")
	b.WriteString("ScriptType: v4.00+\n")
	b.WriteString("PlayResX: 1920\n")
	b.WriteString("PlayResY: 1080\n")
	b.WriteString("WrapStyle: 0\n")
	b.WriteString("ScaledBorderAndShadow: yes\n\n")
	b.WriteString("[V4+ Styles]\n")
	b.WriteString("Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n")
	fmt.Fprintf(&b, "Style: Default,%s,52,&H00F2F2F2,&H000000FF,&H00000000,&H64000000,0,0,0,0,100,100,0,0,1,3.2,0,2,96,96,72,1\n\n", fontName)
	b.WriteString("[Events]\n")
	b.WriteString("Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n")
	for _, c := range cues {
		fmt.Fprintf(&b, "Dialogue: 0,%s,%s,Default,,0,0,0,,%s\n",
			assTime(c.Start), assTime(c.End), assDialogueText(c.Text))
	}
	return b.String()
}

func assTime(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	cs := int(math.Round(sec * 100))
	h := cs / 360000
	cs %= 360000
	m := cs / 6000
	cs %= 6000
	s := cs / 100
	cs %= 100
	return fmt.Sprintf("%d:%02d:%02d.%02d", h, m, s, cs)
}

func assDialogueText(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "{", `\{`)
	s = strings.ReplaceAll(s, "}", `\}`)
	s = strings.ReplaceAll(s, "\n", `\N`)
	return s
}
