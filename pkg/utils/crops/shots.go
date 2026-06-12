package crops

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// Shot represents one segment in a multicam shot list.
// Each shot uses a specific crop region for a time range within the clip.
type Shot struct {
	CropID        string          `json:"crop_id"`
	Start         float64         `json:"start"`
	End           float64         `json:"end"`
	TransitionOut *ShotTransition `json:"transition_out,omitempty"`
}

// ShotTransition describes how one shot hands off to the next.
type ShotTransition struct {
	Type     string  `json:"type"`     // xfade name: "fade", "dissolve", "wipeleft", etc.
	Duration float64 `json:"duration"` // seconds
}

// ShotList is a slice of Shot that implements sql.Scanner and driver.Valuer.
type ShotList []Shot

func (s *ShotList) Scan(value interface{}) error {
	if value == nil {
		*s = []Shot{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to scan ShotList: expected []byte, got %T", value)
	}
	var shots []Shot
	if err := json.Unmarshal(bytes, &shots); err != nil {
		return fmt.Errorf("failed to unmarshal ShotList: %w", err)
	}
	*s = shots
	return nil
}

func (s ShotList) Value() (driver.Value, error) {
	if s == nil {
		return []byte("[]"), nil
	}
	return json.Marshal(s)
}

// Validate checks that the shot list is internally consistent:
// no overlaps, all crop IDs reference existing crops, and durations are positive.
func (s ShotList) Validate(clipStart, clipEnd float64, crops CropArray) error {
	cropIDs := make(map[string]bool, len(crops))
	for _, c := range crops {
		cropIDs[c.ID] = true
	}

	for i, shot := range s {
		if shot.Start < 0 {
			return fmt.Errorf("shot[%d]: start (%.3f) is negative", i, shot.Start)
		}
		if shot.End <= shot.Start {
			return fmt.Errorf("shot[%d]: end (%.3f) must be after start (%.3f)", i, shot.End, shot.Start)
		}
		if !cropIDs[shot.CropID] {
			return fmt.Errorf("shot[%d]: crop_id %q not found", i, shot.CropID)
		}
		if shot.TransitionOut != nil {
			if shot.TransitionOut.Duration <= 0 {
				return fmt.Errorf("shot[%d]: transition duration must be positive", i)
			}
			shotDur := shot.End - shot.Start
			if shot.TransitionOut.Duration >= shotDur {
				return fmt.Errorf("shot[%d]: transition (%.1fs) must be shorter than shot (%.1fs)", i, shot.TransitionOut.Duration, shotDur)
			}
		}
		if i > 0 && shot.Start < s[i-1].End {
			return fmt.Errorf("shot[%d]: start (%.3f) overlaps previous shot end (%.3f)", i, shot.Start, s[i-1].End)
		}
	}
	return nil
}
