package crops

import "fmt"

// LooksLikeFullFrame returns true if the crop coordinates cover the entire frame.
// Used to skip no-op crop filters.
func LooksLikeFullFrame(x, y, w, h float64) bool {
	return x >= 0.49 && x <= 0.51 && y >= 0.49 && y <= 0.51 && w >= 0.99 && h >= 0.99
}

// FFmpegCropFilter returns an ffmpeg crop filter string from normalized coordinates.
// centerX, centerY: center of crop region (0.0-1.0)
// width, height: size of crop region (0.0-1.0)
// Returns an empty string if the crop covers the full frame.
func FFmpegCropFilter(centerX, centerY, width, height float64) string {
	if LooksLikeFullFrame(centerX, centerY, width, height) {
		return ""
	}
	// Convert center+size to top-left corner for FFmpeg crop filter
	// FFmpeg crop syntax: crop=out_w:out_h:x:y (using input dimensions via iw/ih)
	topLeftX := centerX - width/2
	topLeftY := centerY - height/2
	return fmt.Sprintf("crop=iw*%.6f:ih*%.6f:iw*%.6f:ih*%.6f", width, height, topLeftX, topLeftY)
}

// BuildCropFilterByID finds a crop by ID in a CropArray and returns its ffmpeg filter string.
// Returns an empty string if the crop ID is not found or covers the full frame.
func BuildCropFilterByID(cropsData CropArray, cropID string) string {
	for _, crop := range cropsData {
		if crop.ID == cropID {
			return FFmpegCropFilter(crop.X, crop.Y, crop.Width, crop.Height)
		}
	}
	return ""
}
