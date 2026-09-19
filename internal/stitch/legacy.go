package stitch

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// FromLegacy imports encoder stitch segments, separating source time from project placement.
func FromLegacy(title, format, quality string, segments, filters json.RawMessage) (Document, error) {
	d := Document{Version: CurrentVersion, Title: title, FPS: 30, Width: 1920, Height: 1080, Settings: Settings{Format: format, Quality: quality, Filter: cloneRaw(filters)}}
	var raw []map[string]json.RawMessage
	if len(segments) > 0 {
		if e := json.Unmarshal(segments, &raw); e != nil {
			return d, e
		}
	}
	var cursor, prevDuration int64
	for i, m := range raw {
		s := Segment{}
		s.Legacy, _ = json.Marshal(m)
		_ = json.Unmarshal(m["id"], &s.ID)
		_ = json.Unmarshal(m["type"], &s.Type)
		_ = json.Unmarshal(m["video_id"], &s.VideoID)
		_ = json.Unmarshal(m["clip_id"], &s.ClipID)
		_ = json.Unmarshal(m["export_job_id"], &s.ExportJobID)
		_ = json.Unmarshal(m["text"], &s.Text)
		if s.Type == "title" && s.Text == "" {
			_ = json.Unmarshal(m["title"], &s.Text)
		}
		if layoutRaw := m["layout"]; len(layoutRaw) > 0 && string(layoutRaw) != "null" {
			var layout TeaserLayout
			if err := json.Unmarshal(layoutRaw, &layout); err != nil {
				return d, fmt.Errorf("invalid layout: %w", err)
			}
			if err := layout.Validate(); err != nil {
				return d, err
			}
			s.Layout = &layout
		}
		a, ok := number(m["start_ts"])
		if !ok && len(m["start_ts"]) > 0 {
			return d, fmt.Errorf("invalid start_ts")
		}
		z, _ := number(m["duration"])
		end, endOK := number(m["end_ts"])
		if endOK && end >= a {
			z = end - a
		}
		s.SourceInUS = int64(a * 1e6)
		s.DurationUS = int64(z * 1e6)
		if s.DurationUS <= 0 {
			return d, errLegacy
		}
		if s.ID == "" {
			s.ID = newID()
		}
		if tr, ok := m["transition"]; ok && len(tr) > 0 && string(tr) != "null" && string(tr) != "\"\"" {
			var t map[string]json.RawMessage
			_ = json.Unmarshal(tr, &t)
			td, _ := number(t["duration"])
			var kind string
			_ = json.Unmarshal(t["type"], &kind)
			if td > 0 {
				over := int64(td * 1e6)
				if i == 0 || over > prevDuration || over > s.DurationUS {
					return d, fmt.Errorf("transition overlap exceeds range")
				}
				s.Transition = &Transition{Kind: kind, DurationUS: over}
				cursor -= over
			}
		}
		if cursor < 0 {
			return d, fmt.Errorf("negative project placement")
		}
		s.StartUS = cursor
		cursor += s.DurationUS
		prevDuration = s.DurationUS
		d.Segments = append(d.Segments, s)
	}
	return d, Validate(d)
}

var errLegacy = legacyError("segment has no positive duration")

type legacyError string

func (e legacyError) Error() string { return string(e) }

// ToLegacy exports encoder-compatible source seconds and preserves source fields.
func ToLegacy(d Document) (json.RawMessage, json.RawMessage, error) {
	if e := Validate(d); e != nil {
		return nil, nil, e
	}
	out := make([]map[string]json.RawMessage, 0, len(d.Segments))
	for _, s := range d.Segments {
		m := map[string]json.RawMessage{}
		if len(s.Legacy) > 0 {
			_ = json.Unmarshal(s.Legacy, &m)
		}
		put := func(k string, v any) { b, _ := json.Marshal(v); m[k] = b }
		put("id", s.ID)
		put("type", s.Type)
		put("video_id", s.VideoID)
		put("clip_id", s.ClipID)
		put("export_job_id", s.ExportJobID)
		if s.Text != "" {
			put("text", s.Text)
		}
		if s.Layout != nil {
			put("layout", s.Layout)
		}
		put("start_ts", float64(s.SourceInUS)/1e6)
		put("end_ts", float64(s.SourceInUS+s.DurationUS)/1e6)
		put("duration", float64(s.DurationUS)/1e6)
		if s.Transition != nil {
			tr := map[string]json.RawMessage{}
			if old, ok := m["transition"]; ok {
				_ = json.Unmarshal(old, &tr)
				if tr == nil {
					tr = map[string]json.RawMessage{}
				}
			}
			b, _ := json.Marshal(s.Transition.Kind)
			tr["type"] = b
			b, _ = json.Marshal(float64(s.Transition.DurationUS) / 1e6)
			tr["duration"] = b
			put("transition", tr)
		} else {
			delete(m, "transition")
		}
		out = append(out, m)
	}
	b, e := json.Marshal(out)
	if e != nil {
		return nil, nil, e
	}
	f := cloneRaw(d.Settings.Filter)
	if len(f) == 0 {
		f = json.RawMessage("null")
	}
	return b, f, nil
}
func number(v json.RawMessage) (float64, bool) {
	if len(v) == 0 {
		return 0, false
	}
	var n float64
	if json.Unmarshal(v, &n) == nil {
		return n, true
	}
	var s string
	if json.Unmarshal(v, &s) == nil {
		n, e := strconv.ParseFloat(s, 64)
		return n, e == nil
	}
	return 0, false
}
func cloneRaw(v json.RawMessage) json.RawMessage {
	if len(v) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), v...)
}
