package encode

import (
	"encoding/json"
	"fmt"

	"thirdcoast.systems/rewind/internal/jsnum"
	"thirdcoast.systems/rewind/internal/stitch"
)

// canonicalLegacySegments adapts an immutable render snapshot to the legacy
// encoder input.  It deliberately uses only fields in the snapshot; in
// particular, clip IDs are not retained as a cue to reload mutable clip rows.
func canonicalLegacySegments(snapshot stitch.RenderSnapshot) ([]stitchSegmentJSON, error) {
	if len(snapshot.Resolved) == 0 {
		return nil, fmt.Errorf("canonical render snapshot has no segments")
	}
	out := make([]stitchSegmentJSON, 0, len(snapshot.Resolved))
	for _, resolved := range snapshot.Resolved {
		if resolved.DurationUS <= 0 {
			return nil, fmt.Errorf("segment %q has invalid duration", resolved.ID)
		}
		var segment stitchSegmentJSON
		switch resolved.Type {
		case "gap":
			segment = stitchSegmentJSON{Type: "title", Duration: seconds(resolved.DurationUS), BgColor: "#000000"}
		case "title":
			if err := decodeLegacySegment(resolved.Legacy, &segment); err != nil {
				return nil, fmt.Errorf("title segment %q: %w", resolved.ID, err)
			}
			segment.Type = "title"
			segment.Duration = seconds(resolved.DurationUS)
		case "clip", "video":
			if resolved.VideoID == "" {
				return nil, fmt.Errorf("segment %q has no video_id", resolved.ID)
			}
			if len(resolved.Legacy) > 0 {
				if err := decodeLegacySegment(resolved.Legacy, &segment); err != nil {
					return nil, fmt.Errorf("clip segment %q: %w", resolved.ID, err)
				}
			}
			segment.Type = "video"
			segment.VideoID = resolved.VideoID
			segment.ClipID = "" // never let the legacy worker reread a mutable clip
			segment.StartTs = seconds(resolved.SourceInUS)
			segment.EndTs = seconds(resolved.SourceInUS + resolved.DurationUS)
			segment.Duration = seconds(resolved.DurationUS)
		case "stitch":
			if err := decodeLegacySegment(resolved.Legacy, &segment); err != nil && len(resolved.Legacy) > 0 {
				return nil, fmt.Errorf("stitch segment %q: %w", resolved.ID, err)
			}
			segment.Type = "stitch"
			segment.ExportJobID = resolved.ExportJobID
			segment.StartTs = seconds(resolved.SourceInUS)
			segment.EndTs = seconds(resolved.SourceInUS + resolved.DurationUS)
			segment.Duration = seconds(resolved.DurationUS)
		default:
			return nil, fmt.Errorf("unsupported canonical segment type %q", resolved.Type)
		}
		if resolved.Transition != nil {
			tr, err := json.Marshal(stitchTransitionJSON{Type: resolved.Transition.Kind, Duration: seconds(resolved.Transition.DurationUS)})
			if err != nil {
				return nil, fmt.Errorf("segment %q transition: %w", resolved.ID, err)
			}
			segment.RawTransition = tr
		}
		segment.Layout = resolved.Layout
		if converted := encoderCrops(resolved.Crops); len(converted) > 0 {
			segment.Crops = converted
		}
		if converted := encoderShots(resolved.Shots); len(converted) > 0 {
			segment.Shots = converted
		}
		segment.ClipStartUS = resolved.ClipStartUS
		out = append(out, segment)
	}
	return out, nil
}

func decodeLegacySegment(raw json.RawMessage, dst *stitchSegmentJSON) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return err
	}
	var envelope struct {
		Crops       json.RawMessage `json:"crops"`
		FilterStack json.RawMessage `json:"filter_stack"`
	}
	var meta struct {
		Crops       json.RawMessage `json:"crops"`
		FilterStack json.RawMessage `json:"filter_stack"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return err
	}
	if nested, ok := rawObject(raw, "legacy_metadata"); ok {
		if err := json.Unmarshal(nested, &meta); err != nil {
			return fmt.Errorf("legacy_metadata: %w", err)
		}
	}
	if len(meta.Crops) > 0 && string(meta.Crops) != "null" {
		if err := json.Unmarshal(meta.Crops, &dst.Crops); err != nil {
			return fmt.Errorf("crops: %w", err)
		}
	} else if len(envelope.Crops) > 0 && string(envelope.Crops) != "null" {
		if err := json.Unmarshal(envelope.Crops, &dst.Crops); err != nil {
			return fmt.Errorf("crops: %w", err)
		}
	}
	filters := meta.FilterStack
	if len(filters) == 0 || string(filters) == "null" {
		filters = envelope.FilterStack
	}
	if len(filters) > 0 && string(filters) != "null" {
		if err := json.Unmarshal(filters, &dst.Filters); err != nil {
			return fmt.Errorf("filter_stack: %w", err)
		}
	}
	return nil
}

func rawObject(raw json.RawMessage, key string) (json.RawMessage, bool) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return nil, false
	}
	v, ok := obj[key]
	return v, ok
}

func seconds(us int64) jsnum.F { return jsnum.F(float64(us) / 1e6) }

func sliceCanonicalResolved(in []stitch.ResolvedRenderSegment, start, end int64) ([]stitch.ResolvedRenderSegment, error) {
	if end <= start {
		return nil, fmt.Errorf("invalid render range")
	}
	out := make([]stitch.ResolvedRenderSegment, 0, len(in))
	for _, r := range in {
		segEnd := r.StartUS + r.DurationUS
		lo, hi := r.StartUS, segEnd
		if lo < start {
			lo = start
		}
		if hi > end {
			hi = end
		}
		if hi <= lo {
			continue
		}
		copy := r
		if r.Type == "clip" || r.Type == "video" || r.Type == "stitch" {
			copy.SourceInUS += lo - r.StartUS
		}
		copy.StartUS = lo - start
		copy.DurationUS = hi - lo
		if copy.Transition != nil && copy.Transition.DurationUS > copy.DurationUS {
			copy.Transition = nil
		}
		out = append(out, copy)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("render range contains no segments")
	}
	return out, nil
}

func rebaseCanonicalDocument(d stitch.Document, start, end int64) stitch.Document {
	segments := d.Segments[:0]
	for _, segment := range d.Segments {
		segmentEnd := segment.StartUS + segment.DurationUS
		if segmentEnd <= start || segment.StartUS >= end {
			continue
		}
		if segment.StartUS < start {
			if segment.Type == "video" {
				segment.SourceInUS += start - segment.StartUS
			}
			segment.StartUS = start
		}
		if segmentEnd > end {
			segmentEnd = end
		}
		segment.DurationUS = segmentEnd - segment.StartUS
		segment.StartUS -= start
		segments = append(segments, segment)
	}
	d.Segments = segments
	captions := d.Captions[:0]
	for _, caption := range d.Captions {
		if caption.EndUS <= start || caption.StartUS >= end {
			continue
		}
		if caption.StartUS < start {
			caption.StartUS = start
		}
		if caption.EndUS > end {
			caption.EndUS = end
		}
		words := caption.Words[:0]
		for _, word := range caption.Words {
			if word.EndUS <= start || word.StartUS >= end {
				continue
			}
			if word.StartUS < start {
				word.StartUS = start
			}
			if word.EndUS > end {
				word.EndUS = end
			}
			word.StartUS -= start
			word.EndUS -= start
			words = append(words, word)
		}
		caption.Words = words
		caption.StartUS -= start
		caption.EndUS -= start
		captions = append(captions, caption)
	}
	d.Captions = captions
	for i := range d.Overlays {
		if d.Overlays[i].StartUS < start {
			d.Overlays[i].StartUS = start
		}
		if d.Overlays[i].EndUS > end {
			d.Overlays[i].EndUS = end
		}
		d.Overlays[i].StartUS -= start
		d.Overlays[i].EndUS -= start
	}
	return d
}
