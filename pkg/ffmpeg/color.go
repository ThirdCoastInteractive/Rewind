package ffmpeg

import "strings"

// ffmpegColor converts a CSS hex color into an ffmpeg filter color.
// `#` starts a comment in libavfilter graphs, so `#000000` silently
// truncates the rest of the title-card lavfi (no text, often no frames).
func ffmpegColor(c string) string {
	c = strings.TrimSpace(c)
	if c == "" {
		return "white"
	}
	if i := strings.IndexByte(c, '@'); i >= 0 {
		return ffmpegColor(c[:i]) + c[i:]
	}
	if strings.HasPrefix(c, "#") {
		hex := strings.TrimPrefix(c, "#")
		if len(hex) == 3 {
			hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
		}
		return "0x" + strings.ToUpper(hex)
	}
	if len(c) > 2 && (strings.HasPrefix(c, "0x") || strings.HasPrefix(c, "0X")) {
		return "0x" + strings.ToUpper(c[2:])
	}
	return c
}
