package components

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"thirdcoast.systems/rewind/internal/jsnum"
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
	Font        string            `json:"font,omitempty"`
	Position    string            `json:"position,omitempty"`
	BgColor     string            `json:"bg_color,omitempty"`
	TextColor   string            `json:"text_color,omitempty"`
	Audio       string            `json:"audio,omitempty"`
	Role        string            `json:"role,omitempty"`
	Transition  *StitchTransition `json:"transition,omitempty"`
	Filters     []interface{}     `json:"filters,omitempty"`
	GainDb      float64           `json:"gain_db,omitempty"`
	Look        string            `json:"look,omitempty"`
}

func (s *StitchSegment) UnmarshalJSON(data []byte) error {
	type wire struct {
		Type        string            `json:"type"`
		Title       string            `json:"title"`
		Text        string            `json:"text"`
		Subtitle    string            `json:"subtitle"`
		Duration    jsnum.F           `json:"duration"`
		Color       string            `json:"color"`
		ClipID      string            `json:"clip_id"`
		VideoID     string            `json:"video_id"`
		ExportJobID string            `json:"export_job_id"`
		StartTs     jsnum.F           `json:"start_ts"`
		EndTs       jsnum.F           `json:"end_ts"`
		FontSize    jsnum.I           `json:"font_size"`
		Font        string            `json:"font"`
		Position    string            `json:"position"`
		BgColor     string            `json:"bg_color"`
		TextColor   string            `json:"text_color"`
		Audio       string            `json:"audio"`
		Role        string            `json:"role"`
		Transition  *StitchTransition `json:"transition"`
		Filters     []interface{}     `json:"filters"`
		GainDb      jsnum.F           `json:"gain_db"`
		Look        string            `json:"look"`
	}
	var w wire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	*s = StitchSegment{
		Type: w.Type, Title: w.Title, Text: w.Text, Subtitle: w.Subtitle,
		Duration: float64(w.Duration), Color: w.Color, ClipID: w.ClipID, VideoID: w.VideoID,
		ExportJobID: w.ExportJobID, StartTs: float64(w.StartTs), EndTs: float64(w.EndTs),
		FontSize: int(w.FontSize), Font: w.Font, Position: w.Position, BgColor: w.BgColor, TextColor: w.TextColor,
		Audio: w.Audio, Role: w.Role,
		Transition: w.Transition, Filters: w.Filters,
		GainDb: float64(w.GainDb), Look: w.Look,
	}
	return nil
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
	var w struct {
		Type     string  `json:"type"`
		Duration jsnum.F `json:"duration"`
		Outgoing string  `json:"outgoing"`
		Audio    string  `json:"audio"`
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	*t = StitchTransition{Type: w.Type, Duration: float64(w.Duration), Outgoing: w.Outgoing, Audio: w.Audio}
	return nil
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
