package wiki

import (
	"bytes"
	"html"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	gmhtml "github.com/yuin/goldmark/renderer/html"
)

var md = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	goldmark.WithRendererOptions(gmhtml.WithHardWraps()),
)

// RewriteWikilinks turns [[…]] into markdown links to /wiki/{tree}/{slug}.
// Missing pages still link to the show URL (create-stub).
func RewriteWikilinks(src, fromTree string) string {
	if fromTree == "" {
		fromTree = TreeTopic
	}
	return wikiLink.ReplaceAllStringFunc(src, func(m string) string {
		parts := wikiLink.FindStringSubmatch(m)
		if len(parts) < 2 {
			return m
		}
		target := strings.TrimSpace(parts[1])
		label := target
		if len(parts) > 2 && parts[2] != "" {
			label = parts[2]
		}
		tree, slug := ResolveLink(fromTree, target)
		if slug == "" {
			return label
		}
		return "[" + label + "](" + PageURL(tree, slug) + ")"
	})
}

// RenderHTML rewrites wikilinks then converts markdown to HTML via goldmark.
func RenderHTML(body, fromTree string) string {
	return RenderHTMLMedia(body, fromTree, nil)
}

// RenderHTMLMedia is RenderHTML plus inline rewind:// video, clip, and still embeds.
func RenderHTMLMedia(body, fromTree string, lookup ClipLookup) string {
	src := RewriteWikilinks(body, fromTree)
	if strings.TrimSpace(src) == "" {
		return ""
	}
	var buf bytes.Buffer
	if err := md.Convert([]byte(src), &buf); err != nil {
		return "<p>Could not render markdown.</p>"
	}
	return RewriteMediaHTML(buf.String(), lookup)
}

// Decorate fills HTML for a page. Store.Get already loads link rows.
func Decorate(p Page) Page {
	return DecorateMedia(p, nil)
}

// DecorateMedia fills HTML and resolves clip embeds when lookup is set.
func DecorateMedia(p Page, lookup ClipLookup) Page {
	p.HTML = RenderHTMLMedia(p.Body, p.Tree, lookup)
	return p
}

// Heading is a section heading extracted from rendered HTML for the table of contents.
type Heading struct {
	Level int
	ID    string
	Text  string
}

var (
	headingPattern = regexp.MustCompile(`(?is)<h([1-3])([^>]*)>(.*?)</h[1-3]>`)
	idPattern      = regexp.MustCompile(`(?i)\bid="([^"]+)"`)
	tagPattern     = regexp.MustCompile(`<[^>]+>`)
)

// Headings returns h1–h3 entries from rendered HTML, in document order.
func Headings(htmlSrc string) []Heading {
	ms := headingPattern.FindAllStringSubmatch(htmlSrc, -1)
	out := make([]Heading, 0, len(ms))
	for _, m := range ms {
		level := int(m[1][0] - '0')
		id := ""
		if im := idPattern.FindStringSubmatch(m[2]); len(im) > 1 {
			id = im[1]
		}
		text := strings.TrimSpace(tagPattern.ReplaceAllString(m[3], ""))
		text = html.UnescapeString(text)
		if text == "" {
			continue
		}
		if id == "" {
			id = strings.ReplaceAll(Slug(text), "/", "-")
		}
		out = append(out, Heading{Level: level, ID: id, Text: text})
	}
	return out
}

// ArticleHeadings is Headings without the leading page-title h1.
func ArticleHeadings(htmlSrc string) []Heading {
	hs := Headings(htmlSrc)
	out := make([]Heading, 0, len(hs))
	for _, h := range hs {
		if h.Level == 1 {
			continue
		}
		out = append(out, h)
	}
	return out
}
