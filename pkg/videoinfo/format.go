package videoinfo

import (
	"fmt"
	"strings"
)

// FormatProbeDuration converts ffprobe's duration string (seconds) to HH:MM:SS.
func FormatProbeDuration(d string) string {
	var secs float64
	fmt.Sscanf(d, "%f", &secs)
	if secs <= 0 {
		return d
	}
	hours := int(secs) / 3600
	mins := (int(secs) % 3600) / 60
	s := int(secs) % 60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, mins, s)
	}
	return fmt.Sprintf("%d:%02d", mins, s)
}

// FormatProbeBitrate formats an ffprobe bitrate string to human-readable.
func FormatProbeBitrate(br string) string {
	var bps float64
	fmt.Sscanf(br, "%f", &bps)
	if bps <= 0 {
		return br
	}
	if bps >= 1000000 {
		return fmt.Sprintf("%.1f Mbps", bps/1000000)
	}
	return fmt.Sprintf("%.0f kbps", bps/1000)
}

// FormatProbeSize formats an ffprobe size string (bytes) to human-readable.
func FormatProbeSize(s string) string {
	var bytes float64
	fmt.Sscanf(s, "%f", &bytes)
	if bytes <= 0 {
		return s
	}
	if bytes >= 1073741824 {
		return fmt.Sprintf("%.2f GB", bytes/1073741824)
	}
	if bytes >= 1048576 {
		return fmt.Sprintf("%.1f MB", bytes/1048576)
	}
	return fmt.Sprintf("%.0f KB", bytes/1024)
}

// FormatUploadDate formats a YYYYMMDD date string to YYYY-MM-DD.
func FormatUploadDate(d string) string {
	d = strings.TrimSpace(d)
	if len(d) == 8 {
		return d[:4] + "-" + d[4:6] + "-" + d[6:8]
	}
	return d
}

// TruncateURL strips protocol/www and truncates long URLs for display.
func TruncateURL(u string) string {
	u = strings.TrimSpace(u)
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	u = strings.TrimPrefix(u, "www.")
	if len(u) > 50 {
		return u[:47] + "..."
	}
	return u
}
