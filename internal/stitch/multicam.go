package stitch

import (
	"fmt"
	"unicode/utf8"
)

const (
	maxCameraCrops = 16
	maxCameraShots = 200
)

// CameraCrop is a named crop region matching Cut's crop JSON.
type CameraCrop struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	AspectRatio string  `json:"aspect_ratio"`
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
	Width       float64 `json:"width"`
	Height      float64 `json:"height"`
}

// CameraShot is one multicam shot; times are clip-relative seconds (0 = clip in-point).
type CameraShot struct {
	CropID        string                `json:"crop_id"`
	Start         float64               `json:"start"`
	End           float64               `json:"end"`
	TransitionOut *CameraShotTransition `json:"transition_out,omitempty"`
}

// CameraShotTransition describes the handoff to the next shot.
// Type "cut" is stored as a nil transition; otherwise an xfade name.
type CameraShotTransition struct {
	Type     string  `json:"type"`
	Duration float64 `json:"duration"`
}

func validateCameraCrops(crops []CameraCrop) error {
	if len(crops) > maxCameraCrops {
		return fmt.Errorf("too many crops (max %d)", maxCameraCrops)
	}
	ids := make(map[string]bool, len(crops))
	for i, c := range crops {
		if c.ID == "" {
			return fmt.Errorf("crop[%d]: empty id", i)
		}
		if ids[c.ID] {
			return fmt.Errorf("crop[%d]: duplicate id %q", i, c.ID)
		}
		ids[c.ID] = true
		if !finite(c.X) || !finite(c.Y) || !finite(c.Width) || !finite(c.Height) ||
			c.X < 0 || c.Y < 0 || c.Width <= 0 || c.Height <= 0 ||
			c.X+c.Width > 1 || c.Y+c.Height > 1 {
			return fmt.Errorf("crop[%d]: invalid rect", i)
		}
	}
	return nil
}

func validateCameraShots(shots []CameraShot, crops []CameraCrop) error {
	if len(shots) == 0 {
		return nil
	}
	if len(crops) == 0 {
		return fmt.Errorf("shots require at least one crop")
	}
	if len(shots) > maxCameraShots {
		return fmt.Errorf("too many shots (max %d)", maxCameraShots)
	}
	cropIDs := make(map[string]bool, len(crops))
	for _, c := range crops {
		cropIDs[c.ID] = true
	}
	for i, shot := range shots {
		if shot.Start < 0 {
			return fmt.Errorf("shot[%d]: start (%.3f) is negative", i, shot.Start)
		}
		if shot.End <= shot.Start {
			return fmt.Errorf("shot[%d]: end (%.3f) must be after start (%.3f)", i, shot.End, shot.Start)
		}
		if !finite(shot.Start) || !finite(shot.End) {
			return fmt.Errorf("shot[%d]: non-finite range", i)
		}
		if !cropIDs[shot.CropID] {
			return fmt.Errorf("shot[%d]: crop_id %q not found", i, shot.CropID)
		}
		if shot.TransitionOut != nil {
			tr := shot.TransitionOut
			if tr.Duration <= 0 {
				return fmt.Errorf("shot[%d]: transition duration must be positive", i)
			}
			shotDur := shot.End - shot.Start
			if tr.Duration >= shotDur {
				return fmt.Errorf("shot[%d]: transition (%.1fs) must be shorter than shot (%.1fs)", i, tr.Duration, shotDur)
			}
			if tr.Type != "" && utf8.RuneCountInString(tr.Type) > 32 {
				return fmt.Errorf("shot[%d]: transition type too long", i)
			}
		}
		if i > 0 && shot.Start < shots[i-1].End {
			return fmt.Errorf("shot[%d]: start (%.3f) overlaps previous shot end (%.3f)", i, shot.Start, shots[i-1].End)
		}
	}
	return nil
}

func validateSegmentMulticam(s Segment) error {
	if len(s.Crops) == 0 && len(s.Shots) == 0 {
		return nil
	}
	if s.Type == "title" || s.Type == "gap" {
		return fmt.Errorf("multicam is only supported for media segments")
	}
	if err := validateCameraCrops(s.Crops); err != nil {
		return err
	}
	if err := validateCameraShots(s.Shots, s.Crops); err != nil {
		return err
	}
	return nil
}

// VisibleShots returns shots intersected with the segment window, rebased so
// 0 seconds is the source in-point. Empty intersections are dropped.
func VisibleShots(shots []CameraShot, clipStartUS, sourceInUS, durationUS int64) []CameraShot {
	if len(shots) == 0 || durationUS <= 0 {
		return nil
	}
	winStart := float64(sourceInUS-clipStartUS) / 1e6
	winEnd := winStart + float64(durationUS)/1e6
	var out []CameraShot
	for _, shot := range shots {
		start := shot.Start
		end := shot.End
		if end <= winStart || start >= winEnd {
			continue
		}
		if start < winStart {
			start = winStart
		}
		if end > winEnd {
			end = winEnd
		}
		if end <= start {
			continue
		}
		rebased := CameraShot{
			CropID: shot.CropID,
			Start:  start - winStart,
			End:    end - winStart,
		}
		if shot.TransitionOut != nil {
			tr := *shot.TransitionOut
			shotDur := rebased.End - rebased.Start
			if tr.Duration > 0 && shotDur > 0 {
				if tr.Duration >= shotDur {
					tr.Duration = shotDur / 2
				}
				if tr.Duration > 0 && tr.Duration < shotDur {
					rebased.TransitionOut = &tr
				}
			}
		}
		out = append(out, rebased)
	}
	return out
}
