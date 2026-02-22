package ytdlp

import "strings"

// ShouldExpectComments returns true when the extractor is one where we generally
// expect comment extraction to work when requested.
//
// This is intentionally conservative: we only return true for sources we know
// we support well, so jobs don't fail just because a site/extractor doesn't
// expose comments through yt-dlp.
func ShouldExpectComments(info *Info, url string) bool {
	if info != nil {
		if shouldExpectCommentsForExtractor(info.ExtractorKey) {
			return true
		}
		if shouldExpectCommentsForExtractor(info.Extractor) {
			return true
		}
	}

	// Fallback: if we don't have extractor metadata yet, use URL heuristics.
	// Keep this conservative.
	u := strings.ToLower(strings.TrimSpace(url))
	return strings.Contains(u, "youtube.com/") || strings.Contains(u, "youtu.be/")
}

func shouldExpectCommentsForExtractor(extractor string) bool {
	e := strings.ToLower(strings.TrimSpace(extractor))
	if e == "" {
		return false
	}

	// YouTube is the primary target where comment collection is expected.
	if e == "youtube" || strings.HasPrefix(e, "youtube:") || strings.HasPrefix(e, "youtubetab") {
		return true
	}

	return false
}
