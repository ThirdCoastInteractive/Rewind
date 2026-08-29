// Package mediaformat classifies archived videos as short, livestream, or video.
package mediaformat

import "strings"

const (
	Short      = "short"
	Livestream = "livestream"
	Video      = "video"
)

// Classify applies the autopsy precedence rules: live flags, URL hints, then duration.
func Classify(src string, durationSeconds int, wasLive bool, liveStatus string) string {
	ls := strings.ToLower(strings.TrimSpace(liveStatus))
	if wasLive || ls == "was_live" || ls == "is_live" || ls == "post_live" {
		return Livestream
	}
	s := strings.ToLower(src)
	if strings.Contains(s, "/shorts/") || strings.Contains(s, "/short/") {
		return Short
	}
	if durationSeconds > 0 && durationSeconds <= 60 {
		return Short
	}
	if durationSeconds >= 3600 && strings.Contains(s, "rumble.com") && strings.Contains(s, "/livestreams") {
		return Livestream
	}
	return Video
}
