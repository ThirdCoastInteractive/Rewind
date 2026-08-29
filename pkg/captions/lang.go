package captions

import (
	"path/filepath"
	"regexp"
	"strings"
)

var langTagRe = regexp.MustCompile(`^[a-z]{2,3}(?:-[A-Za-z0-9]{2,8})?$`)

// LangFromFilename extracts a BCP-47-ish language tag from a captions filename.
// `uuid.captions.en.vtt` → "en". Unknown or junk tokens (`video`, `captions`)
// fall back to "und".
func LangFromFilename(name string) string {
	base := strings.ToLower(filepath.Base(name))
	base = strings.TrimSuffix(base, ".src.vtt")
	base = strings.TrimSuffix(base, ".vtt")
	parts := strings.Split(base, ".")
	if len(parts) == 0 {
		return "und"
	}
	cand := parts[len(parts)-1]
	if cand == "captions" || cand == "video" || cand == "src" || cand == "auto" {
		return "und"
	}
	// uuid.captions.en
	if cand == "orig" && len(parts) >= 2 {
		cand = strings.TrimSuffix(parts[len(parts)-2], "-orig")
	}
	cand = strings.TrimSuffix(cand, "-orig")
	if langTagRe.MatchString(cand) {
		return cand
	}
	return "und"
}
