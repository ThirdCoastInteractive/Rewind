package crops

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Crop represents a single crop region for a clip
// Coordinates are normalized (0.0-1.0) relative to video dimensions
type Crop struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	AspectRatio string  `json:"aspect_ratio"` // "16:9", "9:16", "1:1", "4:5", "custom"
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
	Width       float64 `json:"width"`
	Height      float64 `json:"height"`
}

// CropArray is a slice of Crop that implements sql.Scanner and driver.Valuer
type CropArray []Crop

// Scan implements sql.Scanner for reading from the database
func (c *CropArray) Scan(value interface{}) error {
	if value == nil {
		*c = []Crop{}
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to scan CropArray: expected []byte, got %T", value)
	}

	var crops []Crop
	if err := json.Unmarshal(bytes, &crops); err != nil {
		return fmt.Errorf("failed to unmarshal CropArray: %w", err)
	}

	*c = crops
	return nil
}

// Value implements driver.Valuer for writing to the database
func (c CropArray) Value() (driver.Value, error) {
	if c == nil {
		return []byte("[]"), nil
	}
	return json.Marshal(c)
}

// CalculateCropForAspectRatio calculates crop coordinates for a given aspect ratio
// Centers the crop in the source video
func CalculateCropForAspectRatio(videoWidth, videoHeight int, aspectRatio, id, name string) Crop {
	var targetWidth, targetHeight float64

	// Try to parse as "w:h" format (e.g., "16:9" or "2.39:1")
	parts := strings.Split(aspectRatio, ":")
	if len(parts) == 2 {
		w, errW := strconv.ParseFloat(parts[0], 64)
		h, errH := strconv.ParseFloat(parts[1], 64)
		if errW == nil && errH == nil && w > 0 && h > 0 {
			targetWidth, targetHeight = w, h
		}
	}

	// If parsing failed, fall back to presets
	if targetWidth == 0 || targetHeight == 0 {
		switch aspectRatio {
		case "16:9":
			targetWidth, targetHeight = 16, 9
		case "9:16":
			targetWidth, targetHeight = 9, 16
		case "1:1":
			targetWidth, targetHeight = 1, 1
		case "4:5":
			targetWidth, targetHeight = 4, 5
		case "4:3":
			targetWidth, targetHeight = 4, 3
		case "21:9":
			targetWidth, targetHeight = 21, 9
		default:
			// Custom/unknown - use full frame (normalized coordinates)
			return Crop{
				ID:          id,
				Name:        name,
				AspectRatio: aspectRatio,
				X:           0.5,
				Y:           0.5,
				Width:       1.0,
				Height:      1.0,
			}
		}
	}

	// Center-crop to target aspect ratio
	targetAspect := targetWidth / targetHeight
	videoAspect := float64(videoWidth) / float64(videoHeight)

	var cropW, cropH, cropX, cropY float64

	if videoAspect > targetAspect {
		// Video is wider - crop sides
		cropH = 1.0
		cropW = targetAspect / videoAspect
		cropX = (1.0 - cropW) / 2.0
		cropY = 0.0
	} else {
		// Video is taller - crop top/bottom
		cropW = 1.0
		cropH = videoAspect / targetAspect
		cropX = 0.0
		cropY = (1.0 - cropH) / 2.0
	}

	return Crop{
		ID:          id,
		Name:        name,
		AspectRatio: aspectRatio,
		X:           cropX + (cropW / 2.0),
		Y:           cropY + (cropH / 2.0),
		Width:       cropW,
		Height:      cropH,
	}
}
