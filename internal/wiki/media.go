package wiki

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// ClipRef is enough to play a saved clip inline.
type ClipRef struct {
	VideoID string
	Title   string
	Start   float64
	End     float64
}

// ClipLookup resolves a clip UUID to its source video and range.
// Nil lookup still renders video embeds; clips become a missing-media card.
type ClipLookup func(id string) (ClipRef, bool)

// Media is a parsed rewind:// video, clip, or still.
type Media struct {
	Kind  string // video | clip
	ID    string
	Asset string // "", "thumbnail", "frame"
	Image string // resolved still URL when Asset is set
	Start float64
	End   float64
	HasStart bool
	HasEnd   bool
}

var (
	uuidPat        = `[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`
	pImgRe      = regexp.MustCompile(`(?is)<p>\s*(<img\b[^>]*>)\s*</p>`)
	imgTagRe    = regexp.MustCompile(`(?is)<img\b[^>]*>`)
	blockLinkRe = regexp.MustCompile(`(?is)<p>\s*<a\b[^>]*\bhref="(rewind://(?:video|clip)/[^"]+)"[^>]*>(.*?)</a>\s*</p>`)
	inlineHrefRe = regexp.MustCompile(`(?i)href="(rewind://[^"]+)"`)
	inlineSrcRe  = regexp.MustCompile(`(?i)src="(rewind://[^"]+)"`)
	attrRe       = regexp.MustCompile(`(?i)\b(src|alt)="([^"]*)"`)
	uuidExact    = regexp.MustCompile(`(?i)^` + uuidPat + `$`)
)

// ParseRewindURI understands rewind://video/{id}, optional /thumbnail or /frame,
// #t=start or #t=start,end, and rewind://clip/{id}.
func ParseRewindURI(raw string) (Media, bool) {
	raw = html.UnescapeString(strings.TrimSpace(raw))
	if !strings.HasPrefix(raw, "rewind://") {
		return Media{}, false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return Media{}, false
	}
	kind := strings.ToLower(u.Host)
	if kind != "video" && kind != "clip" {
		return Media{}, false
	}
	path := strings.Trim(u.Path, "/")
	if path == "" {
		return Media{}, false
	}
	parts := strings.Split(path, "/")
	id := strings.ToLower(parts[0])
	if !isUUID(id) {
		return Media{}, false
	}
	m := Media{Kind: kind, ID: id}
	if kind == "video" && len(parts) > 1 {
		switch strings.ToLower(parts[1]) {
		case "thumbnail":
			m.Asset = "thumbnail"
			m.Image = "/api/videos/" + m.ID + "/thumbnail"
		case "frame":
			m.Asset = "frame"
			t := parseTimeToken(u.Query().Get("t"))
			q := u.Query().Get("quality")
			if q == "" {
				q = "preview"
			}
			m.HasStart = true
			m.Start = t
			m.Image = fmt.Sprintf("/api/videos/%s/frame?t=%.6f&quality=%s", m.ID, t, url.QueryEscape(q))
		}
	}
	if frag := u.Fragment; strings.HasPrefix(frag, "t=") {
		start, end, hasEnd := splitTimeRange(strings.TrimPrefix(frag, "t="))
		m.HasStart = true
		m.Start = start
		if hasEnd {
			m.HasEnd = true
			m.End = end
		}
	}
	return m, true
}

// RewriteMediaHTML turns rewind:// images and block links into inline players or stills,
// and rewrites leftover rewind:// hrefs so they work in a browser.
func RewriteMediaHTML(htmlSrc string, lookup ClipLookup) string {
	if !strings.Contains(htmlSrc, "rewind://") {
		return htmlSrc
	}
	replaceImg := func(tag string) string {
		src, alt := imgAttrs(tag)
		media, ok := ParseRewindURI(src)
		if !ok {
			return ""
		}
		return renderMedia(media, html.UnescapeString(alt), lookup)
	}
	htmlSrc = pImgRe.ReplaceAllStringFunc(htmlSrc, func(m string) string {
		parts := pImgRe.FindStringSubmatch(m)
		if len(parts) < 2 {
			return m
		}
		if out := replaceImg(parts[1]); out != "" {
			return out
		}
		return m
	})
	htmlSrc = imgTagRe.ReplaceAllStringFunc(htmlSrc, func(m string) string {
		if out := replaceImg(m); out != "" {
			return out
		}
		return m
	})
	htmlSrc = blockLinkRe.ReplaceAllStringFunc(htmlSrc, func(m string) string {
		parts := blockLinkRe.FindStringSubmatch(m)
		media, ok := ParseRewindURI(parts[1])
		if !ok {
			return m
		}
		label := strings.TrimSpace(tagPattern.ReplaceAllString(parts[2], ""))
		label = html.UnescapeString(label)
		return renderMedia(media, label, lookup)
	})
	htmlSrc = inlineSrcRe.ReplaceAllStringFunc(htmlSrc, func(m string) string {
		parts := inlineSrcRe.FindStringSubmatch(m)
		media, ok := ParseRewindURI(parts[1])
		if !ok {
			return m
		}
		if media.Image != "" {
			return `src="` + html.EscapeString(media.Image) + `"`
		}
		if media.Kind == "video" {
			return `src="` + html.EscapeString("/api/videos/"+media.ID+"/thumbnail") + `"`
		}
		return m
	})
	htmlSrc = inlineHrefRe.ReplaceAllStringFunc(htmlSrc, func(m string) string {
		parts := inlineHrefRe.FindStringSubmatch(m)
		media, ok := ParseRewindURI(parts[1])
		if !ok {
			return m
		}
		return `href="` + html.EscapeString(mediaWebPath(media, lookup)) + `"`
	})
	return htmlSrc
}

func renderMedia(m Media, label string, lookup ClipLookup) string {
	if label == "" {
		label = m.Kind
	}
	if m.Asset != "" && m.Image != "" {
		cap := html.EscapeString(label)
		return `<figure class="wiki-media wiki-media-image"><img src="` + html.EscapeString(m.Image) + `" alt="` + cap + `" loading="lazy"><figcaption>` + cap + `</figcaption></figure>`
	}
	videoID := m.ID
	start, end := m.Start, m.End
	hasStart, hasEnd := m.HasStart, m.HasEnd
	href := "/videos/" + m.ID
	if m.Kind == "clip" {
		if lookup != nil {
			if ref, ok := lookup(m.ID); ok && ref.VideoID != "" {
				videoID = ref.VideoID
				if strings.TrimSpace(ref.Title) != "" && (label == "" || label == "clip") {
					label = ref.Title
				}
				start, end = ref.Start, ref.End
				hasStart, hasEnd = true, true
				href = "/videos/" + videoID
			} else {
				return `<figure class="wiki-media wiki-media-missing"><figcaption>` + html.EscapeString(label) + ` — clip not found</figcaption></figure>`
			}
		} else {
			return `<figure class="wiki-media wiki-media-missing"><figcaption>` + html.EscapeString(label) + ` — clip ` + html.EscapeString(m.ID) + `</figcaption></figure>`
		}
	}
	escLabel := html.EscapeString(label)
	poster := html.EscapeString("/api/videos/" + videoID + "/thumbnail")
	src := html.EscapeString("/api/videos/" + videoID + "/stream")
	startAttr, endAttr := "", ""
	if hasStart {
		startAttr = fmt.Sprintf(` data-start="%.3f"`, start)
	}
	if hasEnd && end > start {
		endAttr = fmt.Sprintf(` data-end="%.3f"`, end)
	}
	rangeLabel := ""
	if hasStart {
		if hasEnd && end > start {
			rangeLabel = ` <span class="wiki-media-range">` + html.EscapeString(formatSec(start)+"–"+formatSec(end)) + `</span>`
		} else if start > 0 {
			rangeLabel = ` <span class="wiki-media-range">` + html.EscapeString(formatSec(start)) + `</span>`
		}
	}
	kind := "video"
	if m.Kind == "clip" {
		kind = "clip"
	}
	return `<figure class="wiki-media wiki-media-` + kind + `"><div class="wiki-player"><video controls playsinline preload="metadata" poster="` + poster + `"` + startAttr + endAttr + `><source src="` + src + `" type="video/mp4"></video></div><figcaption><a href="` + html.EscapeString(href) + `">` + escLabel + `</a>` + rangeLabel + `</figcaption></figure>`
}

func mediaWebPath(m Media, lookup ClipLookup) string {
	if m.Kind == "clip" && lookup != nil {
		if ref, ok := lookup(m.ID); ok && ref.VideoID != "" {
			return "/videos/" + ref.VideoID
		}
	}
	if m.Kind == "video" {
		return "/videos/" + m.ID
	}
	return "/wiki"
}

func imgAttrs(tag string) (src, alt string) {
	for _, a := range attrRe.FindAllStringSubmatch(tag, -1) {
		switch strings.ToLower(a[1]) {
		case "src":
			src = a[2]
		case "alt":
			alt = a[2]
		}
	}
	return src, alt
}

func isUUID(s string) bool {
	return uuidExact.MatchString(s)
}

func splitTimeRange(s string) (start, end float64, hasEnd bool) {
	s = strings.TrimSpace(s)
	a, b, found := strings.Cut(s, ",")
	start = parseTimeToken(a)
	if found {
		end = parseTimeToken(b)
		hasEnd = true
	}
	return start, end, hasEnd
}

func parseTimeToken(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if strings.Count(s, ":") > 0 {
		parts := strings.Split(s, ":")
		var sec float64
		for _, p := range parts {
			n, _ := strconv.ParseFloat(p, 64)
			sec = sec*60 + n
		}
		return sec
	}
	n, _ := strconv.ParseFloat(s, 64)
	return n
}

func formatSec(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	t := int(sec + 0.5)
	h := t / 3600
	m := (t % 3600) / 60
	s := t % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}
