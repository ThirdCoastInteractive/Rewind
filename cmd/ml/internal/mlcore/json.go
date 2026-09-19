package mlcore

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ModelWindow is the JSON object the LLM is asked to emit per window.
type ModelWindow struct {
	Title      string        `json:"title"`
	Summary    string        `json:"summary"`
	Topics     []string      `json:"topics"`
	Entities   []string      `json:"entities"`
	CueStart   int           `json:"cue_start"`
	CueEnd     int           `json:"cue_end"`
	StartTs    *float64      `json:"start_ts,omitempty"`
	EndTs      *float64      `json:"end_ts,omitempty"`
	Confidence *float64      `json:"confidence,omitempty"`
	Hook       string        `json:"hook,omitempty"`
	Shorts     []ModelWindow `json:"shorts,omitempty"`
}

type modelOutput struct {
	Windows []ModelWindow `json:"windows"`
}

// WindowsJSONSchema is the schema description embedded in the system prompt.
const WindowsJSONSchema = `{"windows":[{"title":"string","summary":"string","topics":["string"],"entities":["string"],"cue_start":1,"cue_end":2,"confidence":0.0,"shorts":[{"title":"string","hook":"string","cue_start":1,"cue_end":2,"confidence":0.0}]}]}`

// SystemPrompt instructs the model to emit JSON windows keyed by cue IDs.
func SystemPrompt() string {
	return strings.TrimSpace(`You segment a video transcript into chapter Context Windows and nested Shorts.
Reply with a single JSON object matching this schema (no markdown, no commentary):
` + WindowsJSONSchema + `

Rules:
- cue_start and cue_end are inclusive 1-based cue IDs from the prompt ([c0001 ...]).
- Cover every cue in this chunk with windows. Windows are topic chapters (a few minutes is typical). A long window is allowed only if the topic does not change.
- shorts are nested punchy moments INSIDE a window: typically 12-30 seconds, hard range 8-45. A complete joke, claim, or reaction that could stand alone as a YouTube Short. hook is the spoken line or punch.
- Aim for several shorts per window (2-6 in a typical 5-10 minute chapter) when there are distinct beats. Skip rambling setup. Short cue ranges must sit inside the parent window's cue_start/cue_end.
- Titles are short; summaries are 1-3 sentences.
- topics are 1-5 durable subjects (not chapter titles, not meeting process). Prefer names from the known-topics list in the user message when they apply. If the chunk discusses a listed subject, that topic MUST appear even when the chapter is public comment or procedure. Do not emit paraphrases of a listed name (use "Flock / ALPR", not "Flock Surveillance"). Do not emit procedural labels (minutes, setup, public comment, overview, arguments, mechanisms).
- entities are people, organizations, and places.
- Do not invent cue IDs outside the chunk. Do not replace a chapter window with a short. Chapter windows stay the map of the show; shorts are sub-items, never a substitute for coverage.`)
}

// RepairSystemPrompt is used for the single JSON repair retry.
func RepairSystemPrompt() string {
	return "The previous reply was not valid JSON. Reply with only a JSON object matching this schema, no markdown:\n" + WindowsJSONSchema
}

// ExtractJSONObject strips markdown fences and leading/trailing junk.
func ExtractJSONObject(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return s
	}
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```JSON")
		s = strings.TrimPrefix(s, "```")
		if i := strings.LastIndex(s, "```"); i >= 0 {
			s = s[:i]
		}
		s = strings.TrimSpace(s)
	}
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j > i {
			s = s[i : j+1]
		}
	}
	return strings.TrimSpace(s)
}

// LocalRepairJSON applies one cheap local fix (trailing commas) after extract.
func LocalRepairJSON(raw string) string {
	s := ExtractJSONObject(raw)
	s = strings.ReplaceAll(s, ",]", "]")
	s = strings.ReplaceAll(s, ", }", "}")
	s = strings.ReplaceAll(s, ",}", "}")
	// trailing comma before end of object/array, including newlines
	var b strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] == ',' {
			j := i + 1
			for j < len(runes) && (runes[j] == ' ' || runes[j] == '\n' || runes[j] == '\r' || runes[j] == '\t') {
				j++
			}
			if j < len(runes) && (runes[j] == '}' || runes[j] == ']') {
				continue
			}
		}
		b.WriteRune(runes[i])
	}
	return b.String()
}

// ParseWindowsJSON extracts windows from a model reply.
func ParseWindowsJSON(raw string) ([]ModelWindow, error) {
	s := LocalRepairJSON(raw)
	if s == "" {
		return nil, fmt.Errorf("empty JSON")
	}
	var out modelOutput
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, fmt.Errorf("parse windows JSON: %w", err)
	}
	if out.Windows == nil {
		// maybe a bare array
		var arr []ModelWindow
		if err := json.Unmarshal([]byte(s), &arr); err == nil {
			out.Windows = arr
		}
	}
	if len(out.Windows) == 0 {
		return nil, fmt.Errorf("no windows in JSON")
	}
	for i := range out.Windows {
		out.Windows[i].Title = strings.TrimSpace(out.Windows[i].Title)
		out.Windows[i].Summary = strings.TrimSpace(out.Windows[i].Summary)
		if out.Windows[i].Topics == nil {
			out.Windows[i].Topics = []string{}
		}
		if out.Windows[i].Entities == nil {
			out.Windows[i].Entities = []string{}
		}
	}
	return out.Windows, nil
}

// RepairFunc is invoked at most once when the first parse fails.
type RepairFunc func(raw string) (string, error)

// DecodeWindows parses raw JSON and, on failure, calls repair exactly once.
func DecodeWindows(raw string, repair RepairFunc) ([]ModelWindow, error) {
	wins, err := ParseWindowsJSON(raw)
	if err == nil {
		return wins, nil
	}
	if repair == nil {
		return nil, err
	}
	fixed, rerr := repair(raw)
	if rerr != nil {
		return nil, fmt.Errorf("json repair: %w (original: %v)", rerr, err)
	}
	return ParseWindowsJSON(fixed)
}

// WindowsFromModel maps model windows onto cue timestamps.
func WindowsFromModel(models []ModelWindow, cues []Cue) []Window {
	out := make([]Window, 0, len(models))
	for _, m := range models {
		w := Window{
			Title:    m.Title,
			Summary:  m.Summary,
			Topics:   append([]string(nil), m.Topics...),
			Entities: append([]string(nil), m.Entities...),
			CueStart: m.CueStart,
			CueEnd:   m.CueEnd,
		}
		if m.Confidence != nil {
			w.Confidence = *m.Confidence
		}
		start, end := 0.0, 0.0
		if m.StartTs != nil && m.EndTs != nil && *m.EndTs > *m.StartTs {
			start, end = *m.StartTs, *m.EndTs
			w.HasGeneratedSpan = true
			w.GeneratedStart, w.GeneratedEnd = start, end
		}
		if c, ok := CueByID(cues, m.CueStart); ok {
			if start == 0 && end == 0 {
				start = c.Start
			}
			w.CueStart = c.ID
		}
		if c, ok := CueByID(cues, m.CueEnd); ok {
			if end == 0 || m.EndTs == nil {
				end = c.End
			}
			w.CueEnd = c.ID
		} else if c, ok := CueByID(cues, m.CueStart); ok && end == 0 {
			end = c.End
		}
		if end <= start {
			continue
		}
		w.Start, w.End = start, end
		if !w.HasGeneratedSpan {
			w.GeneratedStart, w.GeneratedEnd = start, end
			w.HasGeneratedSpan = true
		}
		if strings.TrimSpace(w.Title) == "" {
			w.Title = "Untitled"
		}
		w.Hook = strings.TrimSpace(m.Hook)
		if len(m.Shorts) > 0 {
			w.Shorts = WindowsFromModel(m.Shorts, cues)
			for i := range w.Shorts {
				w.Shorts[i].Kind = "short"
			}
		}
		out = append(out, w)
	}
	return out
}
