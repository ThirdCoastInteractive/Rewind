package stitch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

type Operation struct {
	TeaserCaption    *TeaserCaptionOptions `json:"teaser_caption,omitempty"`
	Type             string                `json:"type"`
	TargetID         string                `json:"target_id,omitempty"`
	IDs              []string              `json:"ids,omitempty"`
	BeforeID         string                `json:"before_id,omitempty"`
	DeltaUS          int64                 `json:"delta_us,omitempty"`
	DeltaX           float64               `json:"delta_x,omitempty"`
	DeltaY           float64               `json:"delta_y,omitempty"`
	StartUS          int64                 `json:"start_us,omitempty"`
	EndUS            int64                 `json:"end_us,omitempty"`
	Segment          *Segment              `json:"segment,omitempty"`
	Caption          *Caption              `json:"caption,omitempty"`
	Overlay          *Overlay              `json:"overlay,omitempty"`
	Settings         *Settings             `json:"settings,omitempty"`
	Group            *Group                `json:"group,omitempty"`
	Title            string                `json:"title,omitempty"`
	ImportSegmentID  string                `json:"import_segment_id,omitempty"`
	ImportLanguage   string                `json:"import_language,omitempty"`
	ImportedCaptions []Caption             `json:"imported_captions,omitempty"`
	Language         string                `json:"language,omitempty"`
	Captions         []Caption             `json:"captions,omitempty"`
	ModelVersion     string                `json:"model_version,omitempty"`
	Layout           *TeaserLayout         `json:"layout,omitempty"`
	Crops            []CameraCrop          `json:"crops,omitempty"`
	Shots            []CameraShot          `json:"shots,omitempty"`
	Width            int                   `json:"width,omitempty"`
	Height           int                   `json:"height,omitempty"`
}

func Apply(d Document, ops []Operation) (Document, []string, error) {
	b, err := json.Marshal(d)
	if err != nil {
		return d, nil, fmt.Errorf("invalid document: %w", err)
	}
	var n Document
	if err = json.Unmarshal(b, &n); err != nil {
		return d, nil, fmt.Errorf("invalid document: %w", err)
	}
	if err = Validate(n); err != nil {
		return d, nil, err
	}
	ob, err := json.Marshal(ops)
	if err != nil {
		return d, nil, fmt.Errorf("invalid operations: %w", err)
	}
	var ownedOps []Operation
	if err = json.Unmarshal(ob, &ownedOps); err != nil {
		return d, nil, fmt.Errorf("invalid operations: %w", err)
	}
	changed := map[string]bool{}
	fail := func(e error) (Document, []string, error) { return d, nil, e }
	findS := func(id string) *Segment {
		for i := range n.Segments {
			if n.Segments[i].ID == id {
				return &n.Segments[i]
			}
		}
		return nil
	}
	findC := func(id string) *Caption {
		for i := range n.Captions {
			if n.Captions[i].ID == id {
				return &n.Captions[i]
			}
		}
		return nil
	}
	findO := func(id string) *Overlay {
		for i := range n.Overlays {
			if n.Overlays[i].ID == id {
				return &n.Overlays[i]
			}
		}
		return nil
	}
	for _, op := range ownedOps {
		switch op.Type {
		case "insert_segment":
			if op.Segment == nil {
				return fail(fmt.Errorf("segment required"))
			}
			seg := clone(*op.Segment)
			if seg.ID == "" {
				seg.ID = newID()
			}
			if findS(seg.ID) != nil {
				return fail(fmt.Errorf("duplicate id"))
			}
			n.Segments = append(n.Segments, seg)
			changed[seg.ID] = true
		case "set_canvas":
			if op.Width < 2 || op.Height < 2 || op.Width > 16384 || op.Height > 16384 || op.Width%2 != 0 || op.Height%2 != 0 {
				return fail(fmt.Errorf("invalid canvas dimensions"))
			}
			n.Width, n.Height = op.Width, op.Height
			changed["canvas"] = true
		case "set_segment_layout":
			s := findS(op.TargetID)
			if s == nil {
				return fail(fmt.Errorf("segment missing"))
			}
			if s.Type == "title" || s.Type == "gap" {
				return fail(fmt.Errorf("layout is only supported for media segments"))
			}
			if err := op.Layout.Validate(); err != nil {
				return fail(err)
			}
			s.Layout = cloneLayout(op.Layout)
			changed[s.ID] = true
		case "set_segment_multicam":
			if op.TargetID == "" {
				return fail(fmt.Errorf("target_id required"))
			}
			s := findS(op.TargetID)
			if s == nil {
				return fail(fmt.Errorf("segment missing"))
			}
			if s.Type == "title" || s.Type == "gap" {
				return fail(fmt.Errorf("multicam is only supported for media segments"))
			}
			crops := clone(op.Crops)
			shots := clone(op.Shots)
			if err := validateCameraCrops(crops); err != nil {
				return fail(err)
			}
			if err := validateCameraShots(shots, crops); err != nil {
				return fail(err)
			}
			s.Crops = crops
			s.Shots = shots
			changed[s.ID] = true
		case "import_captions":
			if op.TargetID == "" || len(op.Captions) > 50000 {
				return fail(fmt.Errorf("invalid caption import"))
			}
			if findS(op.TargetID) == nil {
				return fail(fmt.Errorf("segment missing"))
			}
			members := []string{op.TargetID}
			for _, g := range n.TimingLinks {
				if contains(g.Members, op.TargetID) {
					members = append([]string(nil), g.Members...)
					break
				}
			}
			for _, capn := range op.Captions {
				duplicate := false
				for _, old := range n.Captions {
					if old.SegmentID == op.TargetID && capn.SourceVideoID != "" && old.SourceVideoID == capn.SourceVideoID && old.SourceStartUS == capn.SourceStartUS && old.SourceEndUS == capn.SourceEndUS && old.Language == capn.Language {
						duplicate = true
						break
					}
				}
				if duplicate {
					continue
				}
				c := clone(capn)
				c.SegmentID = op.TargetID
				if c.ID == "" {
					c.ID = newID()
				}
				if c.Language == "" {
					c.Language = op.Language
				}
				if c.Alignment == "" {
					c.Alignment = "unaligned"
				}
				n.Captions = append(n.Captions, c)
				members = append(members, c.ID)
				changed[c.ID] = true
			}
			if len(members) > 1 {
				gid := newID()
				for i, g := range n.TimingLinks {
					if contains(g.Members, op.TargetID) {
						gid = g.ID
						n.TimingLinks[i].Members = members
						break
					}
				}
				if !containsGroup(n.TimingLinks, gid) {
					n.TimingLinks = append(n.TimingLinks, Group{ID: gid, Members: members})
				}
				changed[gid] = true
			}
			changed[op.TargetID] = true
		case "request_alignment":
			c := findC(op.TargetID)
			if c == nil {
				return fail(fmt.Errorf("caption missing"))
			}
			if c.EndUS-c.StartUS > 120_000_000 {
				return fail(fmt.Errorf("alignment range exceeds 120 seconds"))
			}
			h := sha256.Sum256([]byte(c.ID + "|" + c.SourceVideoID + "|" + fmt.Sprint(c.SourceStartUS) + "|" + fmt.Sprint(c.SourceEndUS) + "|" + c.Text + "|" + fmt.Sprint(c.StartUS) + "|" + fmt.Sprint(c.EndUS) + "|" + op.Language + "|" + op.ModelVersion))
			c.Alignment = "pending"
			c.AlignmentKey = hex.EncodeToString(h[:])
			changed[c.ID] = true
		case "trim_segment":
			s := findS(op.TargetID)
			if s == nil || op.EndUS <= op.StartUS || s.SourceInUS+op.StartUS < 0 || s.StartUS+op.StartUS < 0 {
				return fail(fmt.Errorf("invalid trim"))
			}
			s.SourceInUS += op.StartUS
			s.StartUS += op.StartUS
			s.DurationUS = op.EndUS - op.StartUS
			if s.Transition != nil && s.Transition.DurationUS > s.DurationUS {
				s.Transition = nil
			}
			changed[s.ID] = true
		case "group_range_trim":
			if len(op.IDs) == 0 || op.StartUS < 0 || op.EndUS <= op.StartUS {
				return fail(fmt.Errorf("invalid group trim"))
			}
			for _, id := range op.IDs {
				s := findS(id)
				if s == nil || op.EndUS <= s.StartUS || op.StartUS >= s.StartUS+s.DurationUS {
					return fail(fmt.Errorf("invalid group trim"))
				}
			}
			for _, id := range op.IDs {
				s := findS(id)
				lo, hi := op.StartUS, op.EndUS
				if lo < s.StartUS {
					lo = s.StartUS
				}
				if hi > s.StartUS+s.DurationUS {
					hi = s.StartUS + s.DurationUS
				}
				s.SourceInUS += lo - s.StartUS
				s.StartUS = lo
				s.DurationUS = hi - lo
				if s.Transition != nil && s.Transition.DurationUS > s.DurationUS {
					s.Transition = nil
				}
				changed[id] = true
			}
		case "split_segment":
			s := findS(op.TargetID)
			if s == nil || op.DeltaUS <= 0 || op.DeltaUS >= s.DurationUS {
				return fail(fmt.Errorf("invalid split"))
			}
			id := newID()
			second := clone(*s)
			second.ID = id
			second.StartUS = s.StartUS + op.DeltaUS
			second.SourceInUS = s.SourceInUS + op.DeltaUS
			second.DurationUS = s.DurationUS - op.DeltaUS
			second.Transition = nil
			s.DurationUS = op.DeltaUS
			n.Segments = append(n.Segments, second)
			var rightCaptionIDs []string
			rightCaptionSources := map[string]string{}
			for _, c := range append([]Caption(nil), n.Captions...) {
				if c.SegmentID == s.ID && c.StartUS >= s.StartUS+op.DeltaUS {
					for i := range n.Captions {
						if n.Captions[i].ID == c.ID {
							n.Captions[i].SegmentID = id
							if timingLinked(n, c.ID, s.ID) {
								rightCaptionIDs = append(rightCaptionIDs, c.ID)
								rightCaptionSources[c.ID] = c.ID
							}
							changed[c.ID] = true
							break
						}
					}
				}
				if c.SegmentID == s.ID && c.StartUS < s.StartUS+op.DeltaUS && c.EndUS > s.StartUS+op.DeltaUS {
					x := clone(c)
					x.ID = newID()
					x.SegmentID = id
					n.Captions = append(n.Captions, x)
					if timingLinked(n, c.ID, s.ID) {
						rightCaptionIDs = append(rightCaptionIDs, x.ID)
						rightCaptionSources[x.ID] = c.ID
					}
					changed[x.ID] = true
				}
			}
			if len(rightCaptionIDs) > 0 {
				for gi := range n.TimingLinks {
					g := &n.TimingLinks[gi]
					if !contains(g.Members, s.ID) {
						continue
					}
					members := []string{id}
					for _, captionID := range rightCaptionIDs {
						sourceID := rightCaptionSources[captionID]
						if contains(g.Members, sourceID) {
							members = append(members, captionID)
							if sourceID != captionID {
								continue
							}
							filtered := g.Members[:0]
							for _, member := range g.Members {
								if member != sourceID {
									filtered = append(filtered, member)
								}
							}
							g.Members = filtered
						}
					}
					if len(members) > 1 {
						n.TimingLinks = append(n.TimingLinks, Group{ID: newID(), Members: members})
					}
				}
			}
			for gi := len(n.TimingLinks) - 1; gi >= 0; gi-- {
				if len(n.TimingLinks[gi].Members) < 2 {
					n.TimingLinks = append(n.TimingLinks[:gi], n.TimingLinks[gi+1:]...)
				}
			}
			changed[s.ID] = true
			changed[id] = true
		case "move_segment", "move_item":
			if e := moveTimelineItem(&n, op.TargetID, op.DeltaUS, changed); e != nil {
				return fail(e)
			}
		case "reorder_segments":
			s := findS(op.TargetID)
			if s == nil || (op.BeforeID != "" && findS(op.BeforeID) == nil) {
				return fail(fmt.Errorf("segment missing"))
			}
			for _, g := range n.TimingLinks {
				if contains(g.Members, op.TargetID) && contains(g.Members, op.BeforeID) {
					return fail(fmt.Errorf("reorder conflicts with timing group"))
				}
			}
			moveSegment(&n.Segments, op.TargetID, op.BeforeID)
			changed[op.TargetID] = true
			var cursor int64
			for i := range n.Segments {
				if n.Segments[i].Transition != nil {
					cursor -= n.Segments[i].Transition.DurationUS
				}
				if cursor < 0 {
					return fail(fmt.Errorf("invalid transition placement"))
				}
				delta := cursor - n.Segments[i].StartUS
				if delta != 0 {
					if e := moveTimelineItem(&n, n.Segments[i].ID, delta, changed); e != nil {
						return fail(e)
					}
				}
				cursor += n.Segments[i].DurationUS
			}
		case "duplicate_segment":
			s := findS(op.TargetID)
			if s == nil {
				return fail(fmt.Errorf("segment missing"))
			}
			x := clone(*s)
			x.ID = newID()
			x.Transition = nil
			x.StartUS = s.StartUS + s.DurationUS + op.DeltaUS
			idx := 0
			for i, z := range n.Segments {
				if z.ID == s.ID {
					idx = i + 1
					break
				}
			}
			n.Segments = append(n.Segments, Segment{})
			copy(n.Segments[idx+1:], n.Segments[idx:])
			n.Segments[idx] = x
			later := make([]string, 0, len(n.Segments)-idx-1)
			for i := idx + 1; i < len(n.Segments); i++ {
				later = append(later, n.Segments[i].ID)
			}
			for _, lid := range later {
				if e := moveTimelineItem(&n, lid, x.DurationUS, changed); e != nil {
					return fail(e)
				}
			}
			for _, c := range append([]Caption(nil), n.Captions...) {
				if c.SegmentID == s.ID {
					c.ID = newID()
					c.SegmentID = x.ID
					c.StartUS += x.StartUS - s.StartUS
					c.EndUS += x.StartUS - s.StartUS
					n.Captions = append(n.Captions, c)
				}
			}
			changed[x.ID] = true
		case "remove_segment":
			for _, id := range append(op.IDs, op.TargetID) {
				for i, s := range n.Segments {
					if s.ID == id {
						n.Segments = append(n.Segments[:i], n.Segments[i+1:]...)
						changed[id] = true
						for i := len(n.Captions) - 1; i >= 0; i-- {
							if n.Captions[i].SegmentID == id {
								changed[n.Captions[i].ID] = true
								n.Captions = append(n.Captions[:i], n.Captions[i+1:]...)
							}
						}
						for i := len(n.TimingLinks) - 1; i >= 0; i-- {
							n.TimingLinks[i].Members = removeMember(n.TimingLinks[i].Members, id)
							if len(n.TimingLinks[i].Members) < 2 {
								n.TimingLinks = append(n.TimingLinks[:i], n.TimingLinks[i+1:]...)
							}
						}
						break
					}
				}
			}
		case "upsert_caption":
			if op.Caption == nil {
				return fail(fmt.Errorf("caption required"))
			}
			capn := clone(*op.Caption)
			c := findC(capn.ID)
			if c == nil {
				if capn.ID == "" {
					capn.ID = newID()
				}
				n.Captions = append(n.Captions, capn)
				changed[capn.ID] = true
			} else {
				oldText, oldStart, oldEnd, oldLanguage := c.Text, c.StartUS, c.EndUS, c.Language
				*c = capn
				if oldText != capn.Text || oldStart != capn.StartUS || oldEnd != capn.EndUS || oldLanguage != capn.Language {
					c.Words = nil
					c.Alignment = ""
					c.AlignmentKey = ""
				}
				changed[c.ID] = true
			}
		case "delete_caption":
			for i, c := range n.Captions {
				if c.ID == op.TargetID {
					n.Captions = append(n.Captions[:i], n.Captions[i+1:]...)
					changed[op.TargetID] = true
					for i := len(n.TimingLinks) - 1; i >= 0; i-- {
						n.TimingLinks[i].Members = removeMember(n.TimingLinks[i].Members, op.TargetID)
						if len(n.TimingLinks[i].Members) < 2 {
							n.TimingLinks = append(n.TimingLinks[:i], n.TimingLinks[i+1:]...)
						}
					}
					break
				}
			}
		case "split_caption":
			c := findC(op.TargetID)
			if c == nil || op.DeltaUS <= c.StartUS || op.DeltaUS >= c.EndUS {
				return fail(fmt.Errorf("invalid caption split"))
			}
			x := clone(*c)
			x.ID = newID()
			x.StartUS = op.DeltaUS
			c.EndUS = op.DeltaUS
			x.Words = nil
			x.Alignment = ""
			x.AlignmentKey = ""
			c.Words = nil
			c.Alignment = ""
			c.AlignmentKey = ""
			n.Captions = append(n.Captions, x)
			changed[c.ID] = true
			changed[x.ID] = true
		case "merge_caption":
			if len(op.IDs) < 2 {
				return fail(fmt.Errorf("captions required"))
			}
			a := findC(op.IDs[0])
			if a == nil {
				return fail(fmt.Errorf("caption missing"))
			}
			for _, id := range op.IDs[1:] {
				c := findC(id)
				if c == nil {
					return fail(fmt.Errorf("caption missing"))
				}
				a.Text += " " + c.Text
				if c.EndUS > a.EndUS {
					a.EndUS = c.EndUS
				}
				for i, z := range n.Captions {
					if z.ID == id {
						n.Captions = append(n.Captions[:i], n.Captions[i+1:]...)
						break
					}
				}
			}
			a.Words = nil
			a.Alignment = ""
			a.AlignmentKey = ""
			changed[a.ID] = true
		case "upsert_overlay":
			if op.Overlay == nil {
				return fail(fmt.Errorf("overlay required"))
			}
			ov := clone(*op.Overlay)
			o := findO(ov.ID)
			if o != nil && o.Locked && ov.Locked == o.Locked {
				return fail(fmt.Errorf("overlay locked"))
			}
			if o != nil && o.Locked && !ov.Locked && (o.X != ov.X || o.Y != ov.Y || o.Width != ov.Width || o.Height != ov.Height || o.Rotation != ov.Rotation || o.Opacity != ov.Opacity) {
				return fail(fmt.Errorf("unlock cannot change geometry"))
			}
			if o == nil {
				if ov.ID == "" {
					ov.ID = newID()
				}
				n.Overlays = append(n.Overlays, ov)
				changed[ov.ID] = true
			} else {
				*o = ov
				changed[o.ID] = true
			}
		case "delete_overlay":
			o := findO(op.TargetID)
			if o == nil {
				return fail(fmt.Errorf("overlay missing"))
			}
			if o.Locked {
				return fail(fmt.Errorf("overlay locked"))
			}
			for i, x := range n.Overlays {
				if x.ID == o.ID {
					n.Overlays = append(n.Overlays[:i], n.Overlays[i+1:]...)
					break
				}
			}
			changed[op.TargetID] = true
			for i := len(n.PositionGroups) - 1; i >= 0; i-- {
				n.PositionGroups[i].Members = removeMember(n.PositionGroups[i].Members, op.TargetID)
				if len(n.PositionGroups[i].Members) < 2 {
					n.PositionGroups = append(n.PositionGroups[:i], n.PositionGroups[i+1:]...)
				}
			}
		case "move_overlay":
			o := findO(op.TargetID)
			if o == nil {
				return fail(fmt.Errorf("overlay missing"))
			}
			if o.Locked {
				return fail(fmt.Errorf("overlay locked"))
			}
			for _, g := range n.PositionGroups {
				if contains(g.Members, o.ID) {
					for _, id := range g.Members {
						if x := findO(id); x != nil && x.Locked {
							return fail(fmt.Errorf("overlay group locked"))
						}
					}
				}
			}
			o.X += op.DeltaX
			o.Y += op.DeltaY
			changed[o.ID] = true
			for _, g := range n.PositionGroups {
				if contains(g.Members, o.ID) {
					for _, id := range g.Members {
						if x := findO(id); x != nil && x.ID != o.ID {
							x.X += op.DeltaX
							x.Y += op.DeltaY
							changed[x.ID] = true
						}
					}
				}
			}
		case "link_timing", "unlink_timing":
			if op.Group == nil {
				return fail(fmt.Errorf("group required"))
			}
			n.TimingLinks = groupChange(n.TimingLinks, *op.Group, op.Type == "link_timing")
			changed[op.Group.ID] = true
		case "group_position", "ungroup_position":
			if op.Group == nil {
				return fail(fmt.Errorf("group required"))
			}
			n.PositionGroups = groupChange(n.PositionGroups, *op.Group, op.Type == "group_position")
			changed[op.Group.ID] = true
		case "set_settings":
			if op.Settings == nil {
				return fail(fmt.Errorf("settings required"))
			}
			n.Settings = clone(*op.Settings)
			changed["settings"] = true
		case "update_segment":
			s := findS(op.TargetID)
			if s == nil {
				return fail(fmt.Errorf("segment missing"))
			}
			if s.Type != "title" {
				return fail(fmt.Errorf("only title segments may be updated"))
			}
			if op.Segment != nil && op.Segment.Text != "" {
				s.Text = op.Segment.Text
			}
			if op.Title != "" {
				s.Text = op.Title
			}
			if op.Segment != nil && len(op.Segment.Legacy) > 0 {
				var old, patch map[string]any
				if len(s.Legacy) > 0 {
					_ = json.Unmarshal(s.Legacy, &old)
				}
				if old == nil {
					old = map[string]any{}
				}
				if err := json.Unmarshal(op.Segment.Legacy, &patch); err != nil {
					return fail(err)
				}
				for k, v := range patch {
					old[k] = v
				}
				b, err := json.Marshal(old)
				if err != nil {
					return fail(err)
				}
				s.Legacy = b
			}
			changed[s.ID] = true
		case "set_title":
			n.Title = op.Title
			changed["title"] = true
		default:
			return fail(fmt.Errorf("unknown operation %q", op.Type))
		}
	}
	if e := Validate(n); e != nil {
		return d, nil, e
	}
	out := make([]string, 0, len(changed))
	for id := range changed {
		out = append(out, id)
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return n, out, nil
}
func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
func containsGroup(gs []Group, id string) bool {
	for _, g := range gs {
		if g.ID == id {
			return true
		}
	}
	return false
}
func removeMember(xs []string, id string) []string {
	out := xs[:0]
	for _, x := range xs {
		if x != id {
			out = append(out, x)
		}
	}
	return out
}

func moveTimelineItem(d *Document, target string, delta int64, changed map[string]bool) error {
	known := map[string]bool{}
	for _, s := range d.Segments {
		if s.ID == target {
			known[target] = true
		}
	}
	for _, c := range d.Captions {
		if c.ID == target {
			known[target] = true
		}
	}
	for _, o := range d.Overlays {
		if o.ID == target {
			known[target] = true
		}
	}
	if !known[target] {
		return fmt.Errorf("item missing")
	}
	for changedAgain := true; changedAgain; {
		changedAgain = false
		for _, g := range d.TimingLinks {
			hit := false
			for _, m := range g.Members {
				if known[m] {
					hit = true
				}
			}
			if hit {
				for _, m := range g.Members {
					if !known[m] {
						known[m] = true
						changedAgain = true
					}
				}
			}
		}
	}
	for _, s := range d.Segments {
		if known[s.ID] {
			if s.StartUS+delta < 0 {
				return fmt.Errorf("negative time")
			}
		}
	}
	for _, c := range d.Captions {
		if known[c.ID] {
			if c.StartUS+delta < 0 {
				return fmt.Errorf("negative caption time")
			}
		}
	}
	for _, o := range d.Overlays {
		if known[o.ID] && o.Locked {
			return fmt.Errorf("overlay locked")
		}
	}
	for i := range d.Segments {
		if known[d.Segments[i].ID] {
			d.Segments[i].StartUS += delta
			changed[d.Segments[i].ID] = true
		}
	}
	for i := range d.Captions {
		if known[d.Captions[i].ID] {
			d.Captions[i].StartUS += delta
			d.Captions[i].EndUS += delta
			for w := range d.Captions[i].Words {
				d.Captions[i].Words[w].StartUS += delta
				d.Captions[i].Words[w].EndUS += delta
			}
			changed[d.Captions[i].ID] = true
		}
	}
	for i := range d.Overlays {
		if known[d.Overlays[i].ID] {
			d.Overlays[i].StartUS += delta
			d.Overlays[i].EndUS += delta
			changed[d.Overlays[i].ID] = true
		}
	}
	return nil
}

func timingLinked(d Document, a, b string) bool {
	for _, g := range d.TimingLinks {
		if contains(g.Members, a) && contains(g.Members, b) {
			return true
		}
	}
	return false
}
func groupChange(gs []Group, g Group, add bool) []Group {
	for i, x := range gs {
		if x.ID == g.ID {
			if add {
				gs[i] = clone(g)
			} else {
				gs = append(gs[:i], gs[i+1:]...)
			}
			return gs
		}
	}
	if add {
		gs = append(gs, clone(g))
	}
	return gs
}
func moveSegment(xs *[]Segment, id, before string) {
	a := *xs
	var x Segment
	k := -1
	for i, s := range a {
		if s.ID == id {
			x = s
			k = i
		}
	}
	if k < 0 {
		return
	}
	a = append(a[:k], a[k+1:]...)
	j := 0
	for j < len(a) && a[j].ID != before {
		j++
	}
	a = append(a, Segment{})
	copy(a[j+1:], a[j:])
	a[j] = x
	*xs = a
}
