package stitch

import "fmt"

const (
	// DefaultTeaserCanvasWidth is the default portrait teaser width in pixels.
	DefaultTeaserCanvasWidth = 1080
	// DefaultTeaserCanvasHeight is the default portrait teaser height in pixels.
	DefaultTeaserCanvasHeight = 1920

	// TeaserLayoutSingleSpeaker crops one source region to fill the canvas.
	TeaserLayoutSingleSpeaker = "single_speaker"
	// TeaserLayoutTwoSpeakers stacks two source regions vertically.
	TeaserLayoutTwoSpeakers = "two_speakers"
	// TeaserLayoutPreserveScene keeps the source scene visible over a blurred background.
	TeaserLayoutPreserveScene = "preserve_scene"
)

// TeaserCrop is a normalized top-left crop rectangle in source coordinates.
type TeaserCrop struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// TeaserLayout describes how a segment is composed on a portrait teaser canvas.
type TeaserLayout struct {
	Mode  string       `json:"mode"`
	Crops []TeaserCrop `json:"crops"`
}

// Validate checks a teaser layout's mode and normalized crop rectangles.
func (l *TeaserLayout) Validate() error {
	if l == nil {
		return fmt.Errorf("layout is required")
	}
	switch l.Mode {
	case TeaserLayoutSingleSpeaker:
		if len(l.Crops) != 1 {
			return fmt.Errorf("single_speaker layout requires one crop")
		}
	case TeaserLayoutTwoSpeakers:
		if len(l.Crops) != 2 {
			return fmt.Errorf("two_speakers layout requires two crops")
		}
	case TeaserLayoutPreserveScene:
		if len(l.Crops) > 1 {
			return fmt.Errorf("preserve_scene layout accepts at most one crop")
		}
	default:
		return fmt.Errorf("unknown teaser layout mode %q", l.Mode)
	}
	for i, c := range l.Crops {
		if !finite(c.X) || !finite(c.Y) || !finite(c.Width) || !finite(c.Height) || c.X < 0 || c.Y < 0 || c.Width <= 0 || c.Height <= 0 || c.X+c.Width > 1 || c.Y+c.Height > 1 {
			return fmt.Errorf("invalid teaser crop %d", i)
		}
	}
	return nil
}

func cloneLayout(l *TeaserLayout) *TeaserLayout {
	if l == nil {
		return nil
	}
	x := clone(*l)
	return &x
}

// DefaultTeaserLayout returns a full-frame preserve-scene layout.
func DefaultTeaserLayout() *TeaserLayout {
	return &TeaserLayout{Mode: TeaserLayoutPreserveScene}
}
