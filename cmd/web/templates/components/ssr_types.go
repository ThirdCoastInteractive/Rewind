package components

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// StitchSegment mirrors the client-side segment shape sent via DataStar signals.
type StitchSegment struct {
	Type        string            `json:"type"`
	Title       string            `json:"title,omitempty"`
	Text        string            `json:"text,omitempty"`
	Subtitle    string            `json:"subtitle,omitempty"`
	Duration    float64           `json:"duration,omitempty"`
	Color       string            `json:"color,omitempty"`
	ClipID      string            `json:"clip_id,omitempty"`
	VideoID     string            `json:"video_id,omitempty"`
	ExportJobID string            `json:"export_job_id,omitempty"`
	StartTs     float64           `json:"start_ts,omitempty"`
	EndTs       float64           `json:"end_ts,omitempty"`
	FontSize    int               `json:"font_size,omitempty"`
	Position    string            `json:"position,omitempty"`
	BgColor     string            `json:"bg_color,omitempty"`
	TextColor   string            `json:"text_color,omitempty"`
	Transition  *StitchTransition `json:"transition,omitempty"`
	Filters     []interface{}     `json:"filters,omitempty"`
}

// StitchTransition represents a transition between stitch segments.
type StitchTransition struct {
	Type     string  `json:"type"`
	Duration float64 `json:"duration"`
	Outgoing string  `json:"outgoing,omitempty"`
	Audio    string  `json:"audio,omitempty"`
}

// UnmarshalJSON handles the case where DataStar sends transition as an empty
// string (or null) instead of an object. An empty/null value yields a zero
// StitchTransition so the pointer field in StitchSegment stays non-nil but
// effectively empty (Type == "").
func (t *StitchTransition) UnmarshalJSON(data []byte) error {
	s := string(data)
	if s == "null" || s == `""` || s == "" {
		*t = StitchTransition{}
		return nil
	}
	type alias StitchTransition
	var v alias
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*t = StitchTransition(v)
	return nil
}

// TransitionOption is a type/label pair for the transition select dropdown.
type TransitionOption struct {
	Value string
	Label string
}

// StitchTransitionList returns the curated list of transitions.
func StitchTransitionList() []TransitionOption {
	return []TransitionOption{
		{"", "Hard Cut"},
		{"fade", "Fade"},
		{"fadeblack", "Fade Black"},
		{"fadewhite", "Fade White"},
		{"dissolve", "Dissolve"},
		{"wipeleft", "Wipe Left"},
		{"wiperight", "Wipe Right"},
		{"wipeup", "Wipe Up"},
		{"wipedown", "Wipe Down"},
		{"slideleft", "Slide Left"},
		{"slideright", "Slide Right"},
		{"circlecrop", "Circle Crop"},
		{"circleopen", "Circle Open"},
		{"circleclose", "Circle Close"},
		{"radial", "Radial"},
		{"zoomin", "Zoom In"},
	}
}

var trAbbreviations = map[string]string{
	"fade": "fade", "fadeblack": "fblk", "fadewhite": "fwht", "fadefast": "ffst",
	"fadeslow": "fslw", "dissolve": "dslv", "hblur": "hblr", "pixelize": "pxl",
	"wipeleft": "wpl", "wiperight": "wpr", "wipeup": "wpu", "wipedown": "wpd",
	"slideleft": "sll", "slideright": "slr", "slideup": "slu", "slidedown": "sld",
	"smoothleft": "sml", "smoothright": "smr", "coverleft": "cvl", "coverright": "cvr",
	"circlecrop": "circ", "circleopen": "co", "circleclose": "cc",
	"vertopen": "vo", "vertclose": "vc", "horzopen": "ho", "horzclose": "hc",
	"diagtl": "dtl", "diagtr": "dtr", "diagbr": "dbr", "diagbl": "dbl",
	"hlslice": "hsl", "vuslice": "vsl", "radial": "rad", "zoomin": "zoom",
	"squeezeh": "sqzh", "squeezev": "sqzv",
}

func trAbbrev(t string) string {
	if a, ok := trAbbreviations[t]; ok {
		return a
	}
	if len(t) > 4 {
		return t[:4]
	}
	return t
}

func transitionLabel(trType string) string {
	for _, t := range StitchTransitionList() {
		if t.Value == trType {
			return t.Label
		}
	}
	return trType
}

func stitchSegLeftColor(seg StitchSegment) string {
	switch seg.Type {
	case "title":
		return "#f43f5e" // rose-500
	case "stitch":
		return "#f59e0b" // amber-500
	case "video":
		return "#22c55e" // green-500
	default:
		if seg.Color != "" {
			return seg.Color
		}
		return "#3b82f6" // blue-500 (clip)
	}
}

func stitchSegBgClass(seg StitchSegment, selected bool) string {
	switch seg.Type {
	case "title":
		if selected {
			return "bg-rose-500/15"
		}
		return "bg-rose-950/40"
	case "stitch":
		if selected {
			return "bg-amber-500/15"
		}
		return "bg-amber-950/40"
	case "video":
		if selected {
			return "bg-green-500/15"
		}
		return "bg-green-950/40"
	default: // clip
		if selected {
			return "bg-blue-500/15"
		}
		return "bg-blue-950/40"
	}
}

func stitchTotalDuration(segments []StitchSegment) string {
	if len(segments) == 0 {
		return ""
	}
	var total float64
	for _, s := range segments {
		total += s.Duration
	}
	var overlap float64
	for i := 1; i < len(segments); i++ {
		if segments[i].Transition != nil && segments[i].Transition.Duration > 0 {
			overlap += segments[i].Transition.Duration
		}
	}
	total -= overlap
	if total < 0 {
		total = 0
	}
	result := "~" + fmtDuration(total)
	if overlap > 0 {
		result += fmt.Sprintf(" (-%.1fs overlaps)", overlap)
	}
	return result
}

func fmtDuration(s float64) string {
	m := int(s) / 60
	sec := int(s) % 60
	return fmt.Sprintf("%d:%02d", m, sec)
}

func fmtFloat1(v float64) string {
	return fmt.Sprintf("%.1f", v)
}

func segFlexBasis(dur float64) string {
	v := dur
	if v < 0.3 {
		v = 0.3
	}
	return fmt.Sprintf("flex:%s 1 0%%;min-width:48px;", fmtFloat1(v))
}

func segName(seg StitchSegment) string {
	if seg.Title != "" {
		return seg.Title
	}
	if seg.Text != "" {
		return seg.Text
	}
	return "(clip)"
}

// ---- Scrub field helper ----

// ScrubFieldData holds parameters for a scrub input field.
type ScrubFieldData struct {
	ID        string
	Label     string
	Value     float64
	Step      float64
	Min       float64
	Max       float64
	Precision int
}

func scrubDisplayValue(value float64, precision int) string {
	if precision > 0 {
		return fmt.Sprintf("%.*f", precision, value)
	}
	return fmt.Sprintf("%d", int(math.Round(value)))
}

// fmtNum formats a float without trailing zeros.
func fmtNum(v float64) string {
	s := fmt.Sprintf("%.6f", v)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s
}

// ---- Stitch detail helpers ----

func stitchTitleDuration(seg StitchSegment) float64 {
	if seg.Duration > 0 {
		return seg.Duration
	}
	return 3
}

func stitchTitleFontSize(seg StitchSegment) int {
	if seg.FontSize > 0 {
		return seg.FontSize
	}
	return 72
}

func stitchBgColor(seg StitchSegment) string {
	if seg.BgColor != "" {
		return seg.BgColor
	}
	return "#000000"
}

func stitchTextColor(seg StitchSegment) string {
	if seg.TextColor != "" {
		return seg.TextColor
	}
	return "#ffffff"
}

func stitchPosMatch(current, value string) bool {
	if current == "" {
		current = "center"
	}
	return current == value
}

func stitchTrType(seg StitchSegment) string {
	if seg.Transition != nil {
		return seg.Transition.Type
	}
	return ""
}

func stitchTrDuration(seg StitchSegment) float64 {
	if seg.Transition != nil && seg.Transition.Duration > 0 {
		return seg.Transition.Duration
	}
	return 0.5
}

func stitchTrOutgoing(seg StitchSegment) string {
	if seg.Transition != nil && seg.Transition.Outgoing != "" {
		return seg.Transition.Outgoing
	}
	return "play"
}

func stitchTrAudio(seg StitchSegment) string {
	if seg.Transition != nil && seg.Transition.Audio != "" {
		return seg.Transition.Audio
	}
	return "crossfade"
}

