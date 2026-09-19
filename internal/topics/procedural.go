package topics

import "strings"

var proceduralExact = map[string]bool{
	"minutes-approval":                true,
	"technical-setup":                 true,
	"public-comment":                  true,
	"public-forum":                    true,
	"opening-remarks":                 true,
	"episode-overview":                true,
	"committee-of-the-whole-motion":   true,
	"executive-session-release":       true,
	"meeting-start":                   true,
	"community-outcry":                true,
	"committee-criticism":             true,
	"contrarian-arguments":            true,
	"opt-out-mechanisms":              true,
	"risk-assessment":                 true,
	"personal-growth":                 true,
	"logical-fallacies":               true,
	"privacy-advocacy":                true,
	"safety-claims":                   true,
	"podcast-intro":                   true,
	"podcast-intro-and-episode-overview": true,
	"conclusion":                      true,
	"sponsorship":                     true,
	"outro":                           true,
	"recap":                           true,
	"viral-video":                     true,
}

var proceduralTail = map[string]bool{
	"arguments":  true,
	"mechanisms": true,
	"approval":   true,
	"setup":      true,
	"motion":     true,
	"remarks":    true,
	"overview":   true,
	"criticism":  true,
	"comment":    true,
	"forum":      true,
	"growth":     true,
	"assessment": true,
	"intro":        true,
	"introduction": true,
	"conclusion":   true,
	"sponsorship":  true,
	"outro":        true,
	"recap":        true,
}

// IsProcedural reports chapter-glue labels that must not become catalog identities.
func IsProcedural(raw string) bool {
	norm := Normalize(raw)
	if norm == "" {
		return true
	}
	if proceduralExact[norm] {
		return true
	}
	if i := strings.LastIndex(norm, "-"); i >= 0 {
		if proceduralTail[norm[i+1:]] {
			return true
		}
	} else if proceduralTail[norm] {
		return true
	}
	return false
}
