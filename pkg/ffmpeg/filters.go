package ffmpeg

import (
	"fmt"
)

// CropFilter represents a crop filter with normalized coordinates.
type CropFilter struct {
	CenterX, CenterY float64 // Normalized 0.0-1.0 (center of crop region)
	Width, Height    float64 // Normalized 0.0-1.0 (size of crop region)
}

// String returns the ffmpeg filter string.
func (c CropFilter) String() string {
	topLeftX := c.CenterX - c.Width/2
	topLeftY := c.CenterY - c.Height/2
	return fmt.Sprintf("crop=iw*%.6f:ih*%.6f:iw*%.6f:ih*%.6f",
		c.Width, c.Height, topLeftX, topLeftY)
}

// Crop adds a crop filter with normalized coordinates.
// centerX, centerY: center of crop region (0.0-1.0)
// width, height: size of crop region (0.0-1.0)
func Crop(centerX, centerY, width, height float64) Option {
	return Filter(CropFilter{centerX, centerY, width, height}.String())
}

// CropPixels adds a crop filter with pixel coordinates.
func CropPixels(w, h, x, y int) Option {
	return Filter(fmt.Sprintf("crop=%d:%d:%d:%d", w, h, x, y))
}

// ScaleFilter represents a scale filter.
type ScaleFilter struct {
	Width  int // Use -1 or -2 for auto-calculate maintaining aspect ratio
	Height int // Use -2 to ensure even dimensions (required for h264)
}

// String returns the ffmpeg filter string.
func (s ScaleFilter) String() string {
	return fmt.Sprintf("scale=%d:%d", s.Width, s.Height)
}

// Scale adds a scale filter.
// Use -2 for width or height to auto-calculate while maintaining aspect ratio
// and ensuring even dimensions (required for h264).
func Scale(width, height int) Option {
	return Filter(ScaleFilter{width, height}.String())
}

// ScaleWidth scales to a specific width, auto-calculating height with even dimensions.
func ScaleWidth(width int) Option {
	return Scale(width, -2)
}

// ScaleHeight scales to a specific height, auto-calculating width with even dimensions.
func ScaleHeight(height int) Option {
	return Scale(-2, height)
}

// FPS adds an fps filter to change frame rate.
func FPS(rate float64) Option {
	return Filter(fmt.Sprintf("fps=%g", rate))
}

// Tile creates a tile filter for sprite sheets.
func Tile(cols, rows int) Option {
	return Filter(fmt.Sprintf("tile=%dx%d", cols, rows))
}

// ScaleForceAspect scales with force_original_aspect_ratio option.
// mode can be "increase", "decrease", or "disable".
func ScaleForceAspect(width, height int, mode string) Option {
	return Filter(fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=%s", width, height, mode))
}

// Pad adds padding to reach target dimensions.
// Use -1 for width/height to keep original.
// x, y are the position of the input video in the padded output.
func Pad(width, height int, x, y string) Option {
	return Filter(fmt.Sprintf("pad=%d:%d:%s:%s", width, height, x, y))
}

// PadCenter adds padding to center the video in the target dimensions.
func PadCenter(width, height int) Option {
	return Filter(fmt.Sprintf("pad=%d:%d:(ow-iw)/2:(oh-ih)/2", width, height))
}

// EvenDimensions ensures output dimensions are divisible by 2 (required for h264).
// This should be applied after any crop filter that may produce odd dimensions.
func EvenDimensions() Option {
	return Filter("scale=trunc(iw/2)*2:trunc(ih/2)*2")
}
