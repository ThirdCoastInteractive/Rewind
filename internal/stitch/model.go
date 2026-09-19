// Package stitch contains the versioned, deterministic core model for the Stitch editor.
package stitch

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

const CurrentVersion = 1
const maxDocumentUS int64 = 7 * 24 * 60 * 60 * 1_000_000

type Document struct {
	Version        int       `json:"version"`
	Title          string    `json:"title"`
	FPS            float64   `json:"fps"`
	Width          int       `json:"width"`
	Height         int       `json:"height"`
	Segments       []Segment `json:"segments"`
	Captions       []Caption `json:"captions"`
	Overlays       []Overlay `json:"overlays"`
	TimingLinks    []Group   `json:"timing_links"`
	PositionGroups []Group   `json:"position_groups"`
	Settings       Settings  `json:"settings"`
}
type Segment struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	VideoID     string          `json:"video_id"`
	ClipID      string          `json:"clip_id"`
	ExportJobID string          `json:"export_job_id"`
	Text        string          `json:"text,omitempty"`
	StartUS     int64           `json:"start_us"`
	SourceInUS  int64           `json:"source_in_us"`
	DurationUS  int64           `json:"duration_us"`
	Transition  *Transition     `json:"transition,omitempty"`
	Layout      *TeaserLayout   `json:"layout,omitempty"`
	Crops       []CameraCrop    `json:"crops,omitempty"`
	Shots       []CameraShot    `json:"shots,omitempty"`
	ClipStartUS int64           `json:"clip_start_us,omitempty"`
	Legacy      json.RawMessage `json:"legacy,omitempty"`
}
type Transition struct {
	Kind       string `json:"kind"`
	DurationUS int64  `json:"duration_us"`
}
type Caption struct {
	ID            string       `json:"id"`
	SegmentID     string       `json:"segment_id"`
	Text          string       `json:"text"`
	Language      string       `json:"language"`
	StartUS       int64        `json:"start_us"`
	EndUS         int64        `json:"end_us"`
	Words         []Word       `json:"words,omitempty"`
	Style         CaptionStyle `json:"style"`
	SourceHash    string       `json:"source_hash"`
	Alignment     string       `json:"alignment"`
	SourceVideoID string       `json:"source_video_id"`
	SourceStartUS int64        `json:"source_start_us"`
	SourceEndUS   int64        `json:"source_end_us"`
	AlignmentKey  string       `json:"alignment_key"`
}
type Word struct {
	Text    string `json:"text"`
	StartUS int64  `json:"start_us"`
	EndUS   int64  `json:"end_us"`
}
type CaptionStyle struct {
	Font           string  `json:"font"`
	Color          string  `json:"color"`
	Background     string  `json:"background"`
	FontSize       float64 `json:"font_size"`
	Bold           bool    `json:"bold"`
	Italic         bool    `json:"italic"`
	X              float64 `json:"x"`
	Y              float64 `json:"y"`
	OutlineColor   string  `json:"outline_color"`
	OutlineWidth   float64 `json:"outline_width"`
	HighlightColor string  `json:"highlight_color"`
	WordHighlight  bool    `json:"word_highlight"`
	SafeArea       bool    `json:"safe_area"`
}
type Overlay struct {
	ID         string  `json:"id"`
	Kind       string  `json:"kind"`
	Text       string  `json:"text"`
	AssetID    string  `json:"asset_id"`
	StartUS    int64   `json:"start_us"`
	EndUS      int64   `json:"end_us"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Width      float64 `json:"width"`
	Height     float64 `json:"height"`
	Rotation   float64 `json:"rotation"`
	Opacity    float64 `json:"opacity"`
	Z          int     `json:"z"`
	Visible    bool    `json:"visible"`
	Locked     bool    `json:"locked"`
	Color      string  `json:"color"`
	Background string  `json:"background"`
	Font       string  `json:"font"`
	FontSize   float64 `json:"font_size"`
	Points     []Point `json:"points,omitempty"`
}
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}
type Group struct {
	ID      string   `json:"id"`
	Members []string `json:"members"`
}
type Settings struct {
	Quality        string          `json:"quality"`
	Format         string          `json:"format"`
	Background     string          `json:"background"`
	AudioGain      float64         `json:"audio_gain"`
	Filter         json.RawMessage `json:"filter,omitempty"`
	Crop           json.RawMessage `json:"crop,omitempty"`
	Look           json.RawMessage `json:"look,omitempty"`
	CaptionMode    string          `json:"caption_mode"`
	CaptionFont    string          `json:"caption_font"`
	LoudnessTarget float64         `json:"loudness_target"`
}
type Actor struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name"`
}
type ResolvedSegment struct {
	Segment Segment `json:"segment"`
	EndUS   int64   `json:"end_us"`
	Track   int     `json:"track"`
}

func newID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic("stitch: crypto/rand unavailable")
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return hex.EncodeToString(b[:4]) + "-" + hex.EncodeToString(b[4:6]) + "-" + hex.EncodeToString(b[6:8]) + "-" + hex.EncodeToString(b[8:10]) + "-" + hex.EncodeToString(b[10:])
}
func clone[T any](v T) T { b, _ := json.Marshal(v); var out T; _ = json.Unmarshal(b, &out); return out }

func Validate(d Document) error {
	if d.Version != 0 && d.Version != CurrentVersion {
		return fmt.Errorf("unsupported version %d", d.Version)
	}
	if d.FPS <= 0 || d.FPS > 120 || math.IsNaN(d.FPS) || math.IsInf(d.FPS, 0) {
		return fmt.Errorf("invalid fps")
	}
	if d.Width <= 0 || d.Height <= 0 || d.Width > 16384 || d.Height > 16384 {
		return fmt.Errorf("invalid dimensions")
	}
	if len(d.Segments) > 10000 || len(d.Captions) > 50000 || len(d.Overlays) > 50000 {
		return fmt.Errorf("document exceeds bounded counts")
	}
	ids := map[string]bool{}
	segmentIDs := map[string]bool{}
	knownSeg := map[string]bool{"clip": true, "video": true, "stitch": true, "title": true, "gap": true}
	knownOverlay := map[string]bool{"text": true, "image": true, "callout": true, "arrow": true, "rectangle": true, "rect": true, "ellipse": true, "freehand": true}
	for _, s := range d.Segments {
		if s.ID == "" || ids[s.ID] {
			return fmt.Errorf("duplicate or empty segment id")
		}
		ids[s.ID] = true
		segmentIDs[s.ID] = true
		if s.Type != "" && !knownSeg[s.Type] {
			return fmt.Errorf("unknown segment type %q", s.Type)
		}
		if s.StartUS < 0 || s.DurationUS <= 0 || s.SourceInUS < 0 || s.StartUS > maxDocumentUS || s.SourceInUS > maxDocumentUS || s.DurationUS > maxDocumentUS || s.StartUS > maxDocumentUS-s.DurationUS {
			return fmt.Errorf("invalid segment %s range", s.ID)
		}
		if s.Transition != nil && (s.Transition.DurationUS < 0 || s.Transition.DurationUS > s.DurationUS) {
			return fmt.Errorf("invalid transition")
		}
		if s.Layout != nil {
			if s.Type == "title" || s.Type == "gap" {
				return fmt.Errorf("segment %s: layout is only supported for media segments", s.ID)
			}
			if err := s.Layout.Validate(); err != nil {
				return fmt.Errorf("segment %s: %w", s.ID, err)
			}
		}
		if err := validateSegmentMulticam(s); err != nil {
			return fmt.Errorf("segment %s: %w", s.ID, err)
		}
	}
	ordered := append([]Segment(nil), d.Segments...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].StartUS < ordered[j].StartUS })
	for i := 1; i < len(ordered); i++ {
		prev, cur := ordered[i-1], ordered[i]
		over := prev.StartUS + prev.DurationUS - cur.StartUS
		if over > 0 {
			if cur.Transition == nil || cur.Transition.DurationUS != over || over > prev.DurationUS || over > cur.DurationUS {
				return fmt.Errorf("primary segment overlap")
			}
		} else if cur.Transition != nil && cur.Transition.DurationUS > 0 {
			return fmt.Errorf("transition without overlap")
		}
		for j := 0; j < i-1; j++ {
			if ordered[j].StartUS+ordered[j].DurationUS > cur.StartUS {
				return fmt.Errorf("triple primary overlap")
			}
		}
	}
	for _, c := range d.Captions {
		if c.ID == "" || ids[c.ID] {
			return fmt.Errorf("duplicate or empty caption id")
		}
		ids[c.ID] = true
		if c.SegmentID != "" && !segmentIDs[c.SegmentID] {
			return fmt.Errorf("caption segment missing")
		}
		if c.StartUS < 0 || c.EndUS <= c.StartUS || c.EndUS > maxDocumentUS {
			return fmt.Errorf("invalid caption range")
		}
		last := int64(-1)
		for _, w := range c.Words {
			if w.StartUS < c.StartUS || w.EndUS <= w.StartUS || w.EndUS > c.EndUS || w.StartUS < last {
				return fmt.Errorf("invalid caption words")
			}
			if w.StartUS < last {
				return fmt.Errorf("overlapping caption words")
			}
			last = w.EndUS
		}
		if len(c.Words) > 0 && c.Text == "" {
			return fmt.Errorf("caption words require text")
		}
		if !finite(c.Style.X) || !finite(c.Style.Y) || !finite(c.Style.FontSize) || !finite(c.Style.OutlineWidth) {
			return fmt.Errorf("invalid caption style")
		}
	}
	for _, o := range d.Overlays {
		if o.ID == "" || ids[o.ID] {
			return fmt.Errorf("duplicate or empty overlay id")
		}
		ids[o.ID] = true
		if !knownOverlay[o.Kind] {
			return fmt.Errorf("unknown overlay kind %q", o.Kind)
		}
		if o.StartUS < 0 || o.EndUS <= o.StartUS || o.EndUS > maxDocumentUS {
			return fmt.Errorf("invalid overlay range")
		}
		for _, p := range o.Points {
			if !finite(p.X) || !finite(p.Y) {
				return fmt.Errorf("invalid overlay point")
			}
		}
		if o.Width < 0 || o.Height < 0 || o.Opacity < 0 || o.Opacity > 1 || !finite(o.X) || !finite(o.Y) || !finite(o.Width) || !finite(o.Height) || !finite(o.Rotation) || !finite(o.Opacity) || !finite(o.FontSize) {
			return fmt.Errorf("invalid overlay geometry")
		}
	}
	check := func(gs []Group, allowed map[string]bool) error {
		seen := map[string]bool{}
		for _, g := range gs {
			if g.ID == "" || seen[g.ID] || len(g.Members) < 2 {
				return fmt.Errorf("invalid group")
			}
			seen[g.ID] = true
			m := map[string]bool{}
			for _, x := range g.Members {
				if x == "" || m[x] || !allowed[x] {
					return fmt.Errorf("invalid group member")
				}
				m[x] = true
			}
		}
		return nil
	}
	segAllowed := map[string]bool{}
	timingAllowed := map[string]bool{}
	for _, s := range d.Segments {
		segAllowed[s.ID] = true
		timingAllowed[s.ID] = true
	}
	overlayAllowed := map[string]bool{}
	for _, o := range d.Overlays {
		overlayAllowed[o.ID] = true
		timingAllowed[o.ID] = true
	}
	for _, c := range d.Captions {
		timingAllowed[c.ID] = true
	}
	if e := check(d.TimingLinks, timingAllowed); e != nil {
		return e
	}
	if e := uniqueGroupMembers(d.TimingLinks); e != nil {
		return e
	}
	if e := check(d.PositionGroups, overlayAllowed); e != nil {
		return e
	}
	return uniqueGroupMembers(d.PositionGroups)
}
func uniqueGroupMembers(gs []Group) error {
	seen := map[string]bool{}
	for _, g := range gs {
		for _, m := range g.Members {
			if seen[m] {
				return fmt.Errorf("member belongs to multiple groups")
			}
			seen[m] = true
		}
	}
	return nil
}
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func Resolve(d Document) []ResolvedSegment {
	ordered := append([]Segment(nil), d.Segments...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].StartUS < ordered[j].StartUS })
	out := make([]ResolvedSegment, 0, len(ordered))
	var cursor int64
	gapN := 0
	for _, s := range ordered {
		start := SnapFrame(s.StartUS, d.FPS)
		end := SnapFrame(s.StartUS+s.DurationUS, d.FPS)
		if end <= start {
			end = start + int64(math.Ceil(1e6/d.FPS))
		}
		if start > cursor {
			gap := Segment{ID: fmt.Sprintf("gap-%d", gapN), Type: "gap", StartUS: cursor, DurationUS: start - cursor}
			out = append(out, ResolvedSegment{Segment: gap, EndUS: start})
			gapN++
		}
		s.StartUS = start
		s.SourceInUS = SnapFrame(s.SourceInUS, d.FPS)
		s.DurationUS = end - start
		out = append(out, ResolvedSegment{Segment: s, EndUS: end})
		if end > cursor {
			cursor = end
		}
	}
	return out
}

// ResolvedCaptions returns caption copies clipped to the visible interval of their segment.
func ResolvedCaptions(d Document) []Caption {
	bounds := map[string][2]int64{}
	for _, r := range Resolve(d) {
		if r.Segment.Type != "gap" {
			bounds[r.Segment.ID] = [2]int64{r.Segment.StartUS, r.EndUS}
		}
	}
	out := []Caption{}
	for _, c := range d.Captions {
		b, ok := bounds[c.SegmentID]
		if !ok {
			out = append(out, c)
			continue
		}
		start, end := c.StartUS, c.EndUS
		if start < b[0] {
			start = b[0]
		}
		if end > b[1] {
			end = b[1]
		}
		if end <= start {
			continue
		}
		c.StartUS = start
		c.EndUS = end
		out = append(out, c)
	}
	return out
}
func SnapFrame(us int64, fps float64) int64 {
	if fps <= 0 {
		return us
	}
	return int64(math.Round(float64(us)*fps/1e6) * 1e6 / fps)
}
