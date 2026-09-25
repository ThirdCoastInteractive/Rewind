package captions

import "strings"

// ReadableLines turns a word-timed transcript into the phrase lines the
// watch page shows. Phrase-sized cues, such as a cleaned YouTube track,
// are returned unchanged.
func ReadableLines(cues []Cue) []Cue {
	if !wordTimed(cues) {
		return cues
	}
	out := make([]Cue, 0, len(cues)/8)
	var cur Cue
	open := false
	flush := func() {
		if !open {
			return
		}
		cur.Text = strings.TrimSpace(cur.Text)
		if cur.Text != "" {
			out = append(out, cur)
		}
		open = false
	}
	for _, c := range cues {
		text := strings.TrimSpace(c.Text)
		if text == "" {
			continue
		}
		if !open {
			cur = Cue{Start: c.Start, End: c.End, Text: text}
			open = true
		} else if breakBefore(cur, c) {
			flush()
			cur = Cue{Start: c.Start, End: c.End, Text: text}
			open = true
		} else {
			cur.Text = strings.TrimSpace(cur.Text + " " + text)
			if c.End > cur.End {
				cur.End = c.End
			}
		}
		if sentenceEnd(cur.Text) || len(strings.Fields(cur.Text)) >= 18 {
			flush()
		}
	}
	flush()
	if len(out) == 0 {
		return cues
	}
	return out
}

func wordTimed(cues []Cue) bool {
	if len(cues) < 8 {
		return false
	}
	short := 0
	for _, c := range cues {
		if len(strings.Fields(c.Text)) <= 1 {
			short++
		}
	}
	return short*5 >= len(cues)*4
}

func breakBefore(cur, next Cue) bool {
	if sentenceEnd(cur.Text) {
		return true
	}
	if next.Start-cur.End > 1.2 {
		return true
	}
	joined := strings.TrimSpace(cur.Text + " " + strings.TrimSpace(next.Text))
	return len(strings.Fields(joined)) > 18 || len(joined) > 96
}

func sentenceEnd(text string) bool {
	text = strings.TrimRight(strings.TrimSpace(text), `"'”)]`)
	if text == "" {
		return false
	}
	switch text {
	case "Mr.", "Mrs.", "Ms.", "Dr.", "St.", "Jr.", "Sr.":
		return false
	}
	if strings.HasSuffix(text, " Mr.") || strings.HasSuffix(text, " Mrs.") || strings.HasSuffix(text, " Ms.") || strings.HasSuffix(text, " Dr.") {
		return false
	}
	last := text[len(text)-1]
	return last == '.' || last == '?' || last == '!' || strings.HasSuffix(text, "...")
}
