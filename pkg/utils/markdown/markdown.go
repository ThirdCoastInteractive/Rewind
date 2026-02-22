package markdown

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"html/template"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/microcosm-cc/bluemonday"
	"github.com/russross/blackfriday/v2"
)

// Markdown wraps markdown source code and provides methods to render it.
// We only store the source code in the Database, and this type Scans it to/from a string.
type Markdown struct {
	// Source is the markdown source code.
	Source string
	// renderedHTML caches the HTML content renderedHTML from the markdown source.
	renderedHTML *template.HTML
	// rederedText is the plain text content rendered from the markdown source.
	renderedText *template.HTML
}

var (
	bfRenderer = blackfriday.NewHTMLRenderer(blackfriday.HTMLRendererParameters{
		Flags: blackfriday.Safelink | blackfriday.NofollowLinks | blackfriday.HrefTargetBlank | blackfriday.Smartypants | blackfriday.SmartypantsFractions | blackfriday.SmartypantsDashes | blackfriday.SmartypantsLatexDashes | blackfriday.SmartypantsAngledQuotes | blackfriday.SmartypantsQuotesNBSP,
	})
	bfExtensions = blackfriday.NoIntraEmphasis | blackfriday.Tables | blackfriday.FencedCode | blackfriday.Autolink | blackfriday.Strikethrough | blackfriday.SpaceHeadings | blackfriday.NoEmptyLineBeforeBlock | blackfriday.HeadingIDs | blackfriday.AutoHeadingIDs | blackfriday.DefinitionLists
	policy       = bluemonday.UGCPolicy()
)

func NewMarkdown(source string) (*Markdown, error) {
	if source == "" {
		return &Markdown{Source: ""}, nil
	}
	md := &Markdown{Source: source}

	md.Render()
	return md, nil
}

// Render converts the Markdown Source into sanitized HTML.
func (m *Markdown) Render() template.HTML {
	if m.renderedHTML != nil {
		return *m.renderedHTML
	}

	unsafe := blackfriday.Run([]byte(m.Source),
		blackfriday.WithRenderer(bfRenderer),
		blackfriday.WithExtensions(bfExtensions),
	)
	safe := policy.SanitizeBytes(unsafe)
	html := template.HTML(bytes.TrimSpace(safe))
	m.renderedHTML = &html
	return html
}

func (m *Markdown) PlainText() template.HTML {
	if m.renderedText != nil {
		return *m.renderedText
	}

	// Use bluemonday to remove all tags from the output HTML.
	unsafe := blackfriday.Run([]byte(m.Source),
		blackfriday.WithRenderer(bfRenderer),
		blackfriday.WithExtensions(bfExtensions),
	)

	safe := bytes.TrimSpace(bluemonday.StrictPolicy().SanitizeBytes(unsafe))
	h := template.HTML(safe)
	m.renderedText = &h

	return *m.renderedText
}

// Scan implements sql.Scanner, loading markdown text from the DB.
func (m *Markdown) Scan(src any) error {
	if src == nil {
		m.Source = ""
		m.renderedHTML = nil
		m.renderedText = nil
		return nil
	}
	switch v := src.(type) {
	case string:
		m.Source = v
	case []byte:
		m.Source = string(v)
	default:
		return fmt.Errorf("cannot scan type %T into Markdown", src)
	}
	m.renderedHTML = nil
	m.renderedText = nil
	return nil
}

// Value implements driver.Valuer, writing the markdown text back to the DB.
func (m Markdown) Value() (driver.Value, error) {
	return m.Source, nil
}

// ScanText implements the pgtype.TextScanner interface for pgx v5.
func (m *Markdown) ScanText(v pgtype.Text) error {
	if !v.Valid {
		m.Source = ""
		m.renderedHTML = nil
		m.renderedText = nil
		return nil
	}

	m.Source = v.String
	m.renderedHTML = nil
	m.renderedText = nil
	return nil
}

// TextValue implements the pgtype.TextValuer interface for pgx v5.
func (m Markdown) TextValue() (pgtype.Text, error) {
	return pgtype.Text{String: m.Source, Valid: true}, nil
}

// UnmarshalJSON implements json.Unmarshaler so Markdown can be decoded from JSON.
func (m *Markdown) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("Markdown.UnmarshalJSON: %w", err)
	}
	m.Source = s
	m.renderedHTML = nil
	return nil
}
