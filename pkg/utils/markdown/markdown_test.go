package markdown

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func TestNewMarkdown_Empty(t *testing.T) {
	md, err := NewMarkdown("")
	require.NoError(t, err)
	require.NotNil(t, md)
	require.Equal(t, "", md.Source)
	require.Equal(t, "", strings.TrimSpace(string(md.Render())))
}

func TestMarkdown_Render_Sanitizes(t *testing.T) {
	md, err := NewMarkdown("hello <script>alert(1)</script> **world**")
	require.NoError(t, err)

	html := string(md.Render())
	require.NotContains(t, strings.ToLower(html), "<script")
	require.Contains(t, html, "world")

	// caching path
	html2 := string(md.Render())
	require.Equal(t, html, html2)
}

func TestMarkdown_PlainText(t *testing.T) {
	md, err := NewMarkdown("hello **world**")
	require.NoError(t, err)

	text := string(md.PlainText())
	require.Contains(t, text, "hello")
	require.Contains(t, text, "world")
}

func TestMarkdown_ScanAndText(t *testing.T) {
	var md Markdown
	require.NoError(t, md.Scan(nil))
	require.Equal(t, "", md.Source)

	require.NoError(t, md.Scan("abc"))
	require.Equal(t, "abc", md.Source)

	require.NoError(t, md.Scan([]byte("def")))
	require.Equal(t, "def", md.Source)

	require.Error(t, md.Scan(123))

	require.NoError(t, md.ScanText(pgtype.Text{Valid: false}))
	require.Equal(t, "", md.Source)

	require.NoError(t, md.ScanText(pgtype.Text{String: "ghi", Valid: true}))
	require.Equal(t, "ghi", md.Source)

	tv, err := (Markdown{Source: "jkl"}).TextValue()
	require.NoError(t, err)
	require.True(t, tv.Valid)
	require.Equal(t, "jkl", tv.String)
}

func TestMarkdown_UnmarshalJSON(t *testing.T) {
	var md Markdown
	require.NoError(t, json.Unmarshal([]byte(`"hello"`), &md))
	require.Equal(t, "hello", md.Source)
}
